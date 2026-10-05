// Package operatorkeys judges the operator's own keys, ssh.identities, at
// planning: a file that does not exist is passed over; one that exists is
// used when it is the operator's own regular file without group or other
// access (0600 or 0400), a symbolic link only under the credential file
// rule, parsed without a passphrase, and of a type both transports sign
// with. Every other is skipped with its reason, never its contents. The
// credential built from the keys is the operator's login name and the keys'
// paths, backend builtin-operator-keys.
package operatorkeys

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/adapters/sshkey"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/credfile"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// Backend names the credential the keys make.
const Backend = "builtin-operator-keys"

// maxKeyBytes bounds what is read of one file: an OpenSSH key of the
// largest RSA size is about 13 KiB, so a larger file is no key.
const maxKeyBytes = 64 << 10

// Skip is a key file that exists and is not used.
type Skip struct {
	Path, Reason string
}

// Result is the judgement of the listed files: the keys used, in the
// listed order, the files skipped, and every file examined with what was
// found, for a message when none is left.
type Result struct {
	Keys     []credentials.KeyRef
	Skipped  []Skip
	Examined []string
}

// Judge examines each path in order, ~/ the operator's home. rules are the
// operator's user-scope credential file rules; Judge permits 0400 beside
// 0600 and names the backend itself.
func Judge(rules credfile.Rules, paths []string) Result {
	rules.Scope, rules.BackendName, rules.ReadOnlyAllowed = credfile.ScopeUser, Backend, true
	var out Result
	for _, raw := range paths {
		path := rules.ExpandPath(raw)
		ref, reason, absent := judgeOne(rules, path)
		switch {
		case absent:
			out.Examined = append(out.Examined, path+" (absent)")
		case reason != "":
			out.Skipped = append(out.Skipped, Skip{Path: path, Reason: reason})
			out.Examined = append(out.Examined, path+" (skipped: "+reason+")")
		default:
			out.Keys = append(out.Keys, ref)
			out.Examined = append(out.Examined, path)
		}
	}
	return out
}

// judgeOne opens the file under the rules, so the checks run on the
// descriptor read, and parses what it reads.
func judgeOne(rules credfile.Rules, path string) (credentials.KeyRef, string, bool) {
	f, _, err := rules.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return credentials.KeyRef{}, "", true
		}
		return credentials.KeyRef{}, reasonFor(path, err), false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxKeyBytes+1))
	if err != nil {
		return credentials.KeyRef{}, "unreadable", false
	}
	defer clear(data)
	if len(data) > maxKeyBytes {
		return credentials.KeyRef{}, sshkey.ReasonUnreadable, false
	}
	fingerprint, reason := sshkey.Inspect(data)
	if reason != "" {
		return credentials.KeyRef{}, reason, false
	}
	return credentials.KeyRef{Path: path, Fingerprint: fingerprint}, "", false
}

// reasonFor turns a file rule's refusal into the notice's reason.
func reasonFor(path string, err error) string {
	switch errorcodes.Of(err) {
	case "credential_file_symlink_rejected":
		return "a symbolic link, and security.allow-credential-symlinks is false"
	case "credential_file_not_regular":
		return "not a regular file"
	case "credential_file_owner_mismatch":
		return "not owned by the operator"
	case "credential_file_mode_unsafe":
		if fi, statErr := os.Stat(path); statErr == nil {
			return fmt.Sprintf("mode %04o, not 0600 or 0400", fi.Mode().Perm())
		}
		return "group or other access"
	}
	return "unreadable"
}
