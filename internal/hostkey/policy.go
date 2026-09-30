// Package hostkey owns karvi's single SSH host-key policy and trust-store
// selection. System OpenSSH and native SSH adapters consume this package so
// login, command, and run cannot drift into different security behavior.
package hostkey

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// Mode is the operator-selected host-key verification approach.
type Mode string

const (
	// AcceptNew accepts and persists a key when a host is first seen, then
	// rejects any later mismatch. It is the default policy.
	AcceptNew Mode = "accept-new"
	// Insecure accepts unknown and changed keys. Callers must emit warnings.
	Insecure Mode = "insecure"
	// Secure requires a matching entry to exist before the connection starts.
	Secure Mode = "secure"
)

// Error is a stable, classifiable host-key setup or verification failure.
type Error struct {
	Code string
	Host string
	Path string
	Err  error
}

func (e *Error) Error() string {
	parts := []string{e.Code}
	if e.Host != "" {
		parts = append(parts, "host="+e.Host)
	}
	if e.Path != "" {
		parts = append(parts, "known_hosts="+e.Path)
	}
	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	return strings.Join(parts, ": ")
}
func (e *Error) Unwrap() error { return e.Err }

// ErrorCode returns the registered error code.
func (e *Error) ErrorCode() string { return e.Code }

// Policy is the normalized effective policy used by adapters.
type Policy struct {
	Mode           Mode
	KnownHostsFile string
}

// Parse normalizes a configured mode. The removed values "auto" and "default"
// are rejected rather than aliased so a stale configuration fails visibly.
func Parse(value string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "accept-new":
		return AcceptNew, nil
	case "insecure":
		return Insecure, nil
	case "secure":
		return Secure, nil
	default:
		return "", fmt.Errorf("ssh host-key policy must be accept-new, secure, or insecure; got %q", value)
	}
}

// Resolve selects and validates the karvi-owned trust store: the automatic
// location is ~/.local/share/karvi/known_hosts, in the XDG root the path
// resolver creates at its own mode (0750), which the directory rule
// accepts; an explicit path overrides it. The fallback ~/karvi/known_hosts
// left with the karvi line's fresh start: on a fresh host it was created whenever the XDG root came first, and a
// present ~/karvi then became the operator's base.
func Resolve(modeValue, configuredPath, home string) (Policy, error) {
	mode, err := Parse(modeValue)
	if err != nil {
		return Policy{}, &Error{Code: "host_key_policy_invalid", Err: err}
	}
	if strings.TrimSpace(home) == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return Policy{}, &Error{Code: "host_key_home_unavailable", Err: err}
		}
	}

	explicit := strings.TrimSpace(configuredPath)
	if explicit != "" && !strings.EqualFold(explicit, "auto") {
		path, expandErr := expandHome(explicit, home)
		if expandErr != nil {
			return Policy{}, expandErr
		}
		p := Policy{Mode: mode, KnownHostsFile: filepath.Clean(path)}
		if mode == Insecure {
			// Insecure mode may use a missing file only for best-effort mismatch
			// comparison; the actual SSH connection does not trust this file.
			if _, statErr := os.Lstat(p.KnownHostsFile); errors.Is(statErr, os.ErrNotExist) {
				return p, nil
			}
		}
		if mode == Secure {
			// Report the policy condition before validating the parent directory.
			// Operators selecting secure mode need the actionable answer that the
			// host-key store has not been enrolled, even when its directory has not
			// been created yet.
			if _, statErr := os.Lstat(p.KnownHostsFile); errors.Is(statErr, os.ErrNotExist) {
				return Policy{}, &Error{Code: "host_key_not_enrolled", Path: p.KnownHostsFile, Err: fmt.Errorf("secure mode requires a pre-populated known_hosts file")}
			} else if statErr != nil {
				return Policy{}, &Error{Code: "host_key_trust_store_unavailable", Path: p.KnownHostsFile, Err: statErr}
			}
		}
		if mode == AcceptNew {
			if err := ensureTrustFile(p.KnownHostsFile); err != nil {
				return Policy{}, err
			}
		} else if err := validateTrustFile(p.KnownHostsFile, mode); err != nil {
			return Policy{}, err
		}
		return p, nil
	}

	candidates := []string{
		filepath.Join(home, ".local", "share", "karvi", "known_hosts"),
	}
	for _, candidate := range candidates {
		if _, statErr := os.Lstat(candidate); statErr == nil {
			if err := validateTrustFile(candidate, mode); err != nil {
				return Policy{}, err
			}
			return Policy{Mode: mode, KnownHostsFile: candidate}, nil
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return Policy{}, &Error{Code: "host_key_trust_store_unavailable", Path: candidate, Err: statErr}
		}
	}

	if mode == Secure {
		return Policy{}, &Error{Code: "host_key_not_enrolled", Path: candidates[0], Err: fmt.Errorf("secure mode requires a pre-populated known_hosts file")}
	}
	if mode == Insecure {
		// Return the preferred comparison path without creating persistent
		// state. The caller may still warn that no prior key was available.
		return Policy{Mode: mode, KnownHostsFile: candidates[0]}, nil
	}
	for _, candidate := range candidates {
		if err := ensureTrustFile(candidate); err == nil {
			return Policy{Mode: mode, KnownHostsFile: candidate}, nil
		}
	}
	return Policy{}, &Error{Code: "host_key_trust_store_candidates_exhausted", Path: candidates[0], Err: fmt.Errorf("could not create the karvi trust store")}
}

// OpenSSHSettings maps the normalized policy to managed ssh_config values.
func (p Policy) OpenSSHSettings() (strict, userKnownHosts, globalKnownHosts string) {
	switch p.Mode {
	case Secure:
		return "yes", p.KnownHostsFile, "/dev/null"
	case Insecure:
		return "no", "/dev/null", "/dev/null"
	default:
		return "accept-new", p.KnownHostsFile, "/dev/null"
	}
}

func expandHome(path, home string) (string, error) {
	switch {
	case path == "~":
		return home, nil
	case strings.HasPrefix(path, "~/"):
		return filepath.Join(home, path[2:]), nil
	case strings.HasPrefix(path, "~"):
		return "", errorcodes.Errorf("path_other_user_home_unsupported", "only the current user's ~ expansion is supported: %s", path)
	case filepath.IsAbs(path):
		return path, nil
	default:
		return filepath.Join(home, path), nil
	}
}

// ensureTrustFile prepares the store for accept-new. karvi sets permissions
// only on what it creates: a missing store is created 0600, and a store that
// exists is validated and never changed, so one whose mode is not 0600 is
// refused with host_key_trust_store_permission.
func ensureTrustFile(path string) error {
	dir := filepath.Dir(path)
	if err := ensurePrivateDirectory(dir, path); err != nil {
		return err
	}
	// O_EXCL tells a store this call made from one it found; O_NOFOLLOW keeps
	// a symbolic link at path from being created through.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if errors.Is(err, os.ErrExist) {
		// The store exists: the operator's, an earlier run's, or a concurrent
		// first enrollment's, which created it 0600 a moment ago.
		return validateTrustFile(path, AcceptNew)
	}
	if err != nil {
		return &Error{Code: "host_key_trust_store_create_failed", Path: path, Err: err}
	}
	// The mode is applied to the file just made, through its handle, so the
	// umask cannot narrow it and no path is resolved a second time.
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return &Error{Code: "host_key_trust_store_chmod_failed", Path: path, Err: err}
	}
	if closeErr := f.Close(); closeErr != nil {
		return &Error{Code: "host_key_trust_store_close_failed", Path: path, Err: closeErr}
	}
	return validateTrustFile(path, AcceptNew)
}

func ensurePrivateDirectory(dir, path string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return &Error{Code: "host_key_directory_invalid", Path: path, Err: err}
	}
	return validatePrivateDirectory(dir, path)
}

// validatePrivateDirectory checks the trust-store directory for path; each
// failed rule has its own code.
func validatePrivateDirectory(dir, path string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return &Error{Code: "host_key_directory_invalid", Path: path, Err: err}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return &Error{Code: "host_key_directory_not_real", Path: path, Err: fmt.Errorf("directory must be a real directory, not a symlink")}
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Getuid() {
		return &Error{Code: "host_key_directory_owner", Path: path, Err: fmt.Errorf("directory is owned by UID %d, expected %d", stat.Uid, os.Getuid())}
	}
	// The operator's own root is created 0750 by the path resolver and the
	// store's file is 0600, so the rule is that group and others cannot
	// write the directory, not that its mode is exactly 0700.
	if info.Mode().Perm()&0o022 != 0 {
		return &Error{Code: "host_key_directory_permission", Path: path, Err: fmt.Errorf("directory mode is %04o; group or others may write it; expected 0700 or 0750", info.Mode().Perm())}
	}
	return nil
}

// validateTrustFile checks an existing store and changes nothing. accept-new
// and secure take their trust from the store and require mode 0600; insecure
// takes none (it reads the store only to warn of a mismatch), so it continues
// whatever the file's mode. The directory, the file's type, and its owner are
// checked under every policy.
func validateTrustFile(path string, mode Mode) error {
	if err := validatePrivateDirectory(filepath.Dir(path), path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		code := "host_key_trust_store_unavailable"
		if errors.Is(err, os.ErrNotExist) {
			code = "host_key_not_enrolled"
		}
		return &Error{Code: code, Path: path, Err: err}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return &Error{Code: "host_key_trust_store_invalid", Path: path, Err: fmt.Errorf("known_hosts must be a regular non-symlink file")}
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Getuid() {
		return &Error{Code: "host_key_trust_store_owner", Path: path, Err: fmt.Errorf("file is owned by UID %d, expected %d", stat.Uid, os.Getuid())}
	}
	if mode != Insecure && info.Mode().Perm() != 0o600 {
		return &Error{Code: "host_key_trust_store_permission", Path: path, Err: fmt.Errorf("file mode is %04o; run chmod 600 %s", info.Mode().Perm(), path)}
	}
	return nil
}
