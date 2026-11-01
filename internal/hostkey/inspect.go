package hostkey

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1" // OpenSSH known_hosts hashing is defined as HMAC-SHA1.
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// Comparison describes a live host key compared with enrolled keys.
type Comparison string

const (
	Unknown  Comparison = "unknown"
	Match    Comparison = "match"
	Mismatch Comparison = "mismatch"
)

// Inspection contains only public-key fingerprints, never credential data.
type Inspection struct {
	Comparison            Comparison
	PresentedFingerprints []string
	EnrolledFingerprints  []string
	Presented             []keyRecord
}

type keyRecord struct {
	Type string
	Blob string
}

// KeyMismatch is a key accepted under insecure that differs from the one the
// store holds for the device: the stored and the presented keys' public
// fingerprints, the presented key's only record, since insecure stores
// nothing.
type KeyMismatch struct {
	Enrolled, Presented []string
}

// NotCompared is an insecure comparison that could not complete, the
// connection going on uncompared: Cause the terminal's short word for it
// (one of the NotCompared* causes), Reason the whole of it for the record
// and the audit.
type NotCompared struct {
	Cause, Reason string
}

// The causes of a comparison that could not complete.
const (
	NotComparedNoScanner   = "ssh-keyscan not installed"
	NotComparedTimedOut    = "ssh-keyscan timed out"
	NotComparedScanFailed  = "ssh-keyscan failed"
	NotComparedNoKey       = "no usable key"
	NotComparedStoreUnread = "trust store unreadable"
)

// CompareInsecure compares, under insecure, the keys a device presents with
// the ones the store holds for it, by an ssh-keyscan beside the system
// transport's connection (OpenSSH itself is given /dev/null as its store
// under insecure): host_key_revoked, which refuses the connection, when a
// presented key is one the store marks @revoked for the device; a
// KeyMismatch when they differ; a NotCompared when the comparison could not
// complete; none of them under another policy, when the store holds no line
// for the device, or when a presented key matches.
func CompareInsecure(ctx context.Context, p Policy, host, address string, port int) (*KeyMismatch, *NotCompared, error) {
	if p.Mode != Insecure {
		return nil, nil, nil
	}
	if enrolled, revoked, err := storeKeys(p.KnownHostsFile, []string{Identity(host, port)}); err != nil || len(enrolled)+len(revoked) == 0 {
		return nil, nil, nil
	}
	inspection, err := InspectRemote(ctx, p.KnownHostsFile, host, address, port, 5*time.Second)
	if errorcodes.Of(err) == "host_key_revoked" {
		return nil, nil, err
	}
	if err != nil {
		return nil, &NotCompared{Cause: notComparedCause(err), Reason: err.Error()}, nil
	}
	if inspection.Comparison == Mismatch {
		return &KeyMismatch{Enrolled: inspection.EnrolledFingerprints, Presented: inspection.PresentedFingerprints}, nil, nil
	}
	return nil, nil, nil
}

// notComparedCause is the short cause of an InspectRemote failure.
func notComparedCause(err error) string {
	var timedOut scanTimedOut
	switch {
	case errorcodes.Of(err) == "dependency_ssh_keyscan_unavailable":
		return NotComparedNoScanner
	case errorcodes.Of(err) == "host_key_scan_empty":
		return NotComparedNoKey
	case errorcodes.Of(err) == "host_key_trust_store_unavailable":
		return NotComparedStoreUnread
	case errors.As(err, &timedOut):
		return NotComparedTimedOut
	}
	return NotComparedScanFailed
}

// scanTimedOut is an ssh-keyscan that ended without a key at or past its
// bound: it reports a timeout as it does a refusal (exit 1, no output,
// nothing on standard error), so the time taken tells them apart.
type scanTimedOut struct{ after time.Duration }

func (e scanTimedOut) Error() string {
	return fmt.Sprintf("ssh-keyscan timed out: no key within %s", e.after)
}

// MismatchPhrase is the host_key_mismatch_accepted notice's: insecure
// accepted a key differing from the stored one.
func MismatchPhrase() Phrase {
	return Phrase{Before: "ssh host-key mismatch ", After: " proceeding at risk"}
}

// NotComparedPhrase is the host_key_not_compared notice's, with its short
// cause.
func NotComparedPhrase(cause string) Phrase {
	return Phrase{Before: "ssh host-key ", After: " not compared: " + cause}
}

func mismatchDescription(host string, i Inspection) string {
	return fmt.Sprintf("SSH host key mismatch for %s: enrolled=%s presented=%s", host, strings.Join(i.EnrolledFingerprints, ","), strings.Join(i.PresentedFingerprints, ","))
}

// InspectRemote obtains public host keys with ssh-keyscan and compares them
// numerically/by key blob against matching OpenSSH known_hosts entries; a
// scanned key the store marks @revoked for the device is host_key_revoked.
func InspectRemote(ctx context.Context, knownHostsFile, host, address string, port int, timeout time.Duration) (Inspection, error) {
	scanner, err := exec.LookPath("ssh-keyscan")
	if err != nil {
		return Inspection{}, errorcodes.Errorf("dependency_ssh_keyscan_unavailable", "ssh-keyscan unavailable: %w", err)
	}
	return inspectRemoteWithBinary(ctx, scanner, knownHostsFile, host, address, port, timeout)
}

func inspectRemoteWithBinary(ctx context.Context, scanner, knownHostsFile, host, address string, port int, timeout time.Duration) (Inspection, error) {
	if port == 0 {
		port = 22
	}
	seconds := int((timeout + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 5
	}
	args := []string{"-T", strconv.Itoa(seconds), "-p", strconv.Itoa(port), address}
	cmd := exec.CommandContext(ctx, scanner, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	started := time.Now()
	output, err := cmd.Output()
	presented, parseErr := parseScannedKeys(output)
	if parseErr != nil {
		return Inspection{}, parseErr
	}
	if len(presented) == 0 {
		if err != nil {
			if bound := time.Duration(seconds) * time.Second; time.Since(started) >= bound {
				return Inspection{}, scanTimedOut{after: bound}
			}
			detail := strings.TrimSpace(stderr.String())
			if detail == "" {
				detail = err.Error()
			}
			return Inspection{}, fmt.Errorf("ssh-keyscan failed: %s", detail)
		}
		return Inspection{}, errorcodes.Errorf("host_key_scan_empty", "ssh-keyscan returned no usable host keys")
	}

	enrolled, revoked, readErr := storeKeys(knownHostsFile, []string{Identity(host, port)})
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return Inspection{}, errorcodes.Ensure(readErr, "host_key_trust_store_unavailable")
	}
	if revokedErr := revokedError(host, knownHostsFile, presented, revoked); revokedErr != nil {
		return Inspection{}, revokedErr
	}
	result := Inspection{Presented: presented, PresentedFingerprints: fingerprints(presented), EnrolledFingerprints: fingerprints(enrolled)}
	if len(enrolled) == 0 {
		result.Comparison = Unknown
		return result, nil
	}
	for _, live := range presented {
		for _, stored := range enrolled {
			if live.Type == stored.Type && live.Blob == stored.Blob {
				result.Comparison = Match
				return result, nil
			}
		}
	}
	result.Comparison = Mismatch
	return result, nil
}

// HasEnrolledHost reports whether the store enrolls a key for the device's
// identity on port: a plain line, never one with a marker (storeKeys).
func HasEnrolledHost(file, host string, port int) bool {
	keys, err := matchingKeys(file, []string{Identity(host, port)})
	return err == nil && len(keys) > 0
}

func parseScannedKeys(data []byte) ([]keyRecord, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	out := []keyRecord{}
	seen := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		rec := keyRecord{Type: fields[1], Blob: fields[2]}
		if _, err := base64.StdEncoding.DecodeString(rec.Blob); err != nil {
			return nil, errorcodes.Errorf("host_key_scan_key_invalid", "invalid key data from ssh-keyscan: %w", err)
		}
		key := rec.Type + " " + rec.Blob
		if !seen[key] {
			seen[key] = true
			out = append(out, rec)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type == out[j].Type {
			return out[i].Blob < out[j].Blob
		}
		return out[i].Type < out[j].Type
	})
	return out, nil
}

// matchingKeys is the keys the store enrolls under a candidate: those of
// its plain lines (storeKeys).
func matchingKeys(file string, candidates []string) ([]keyRecord, error) {
	enrolled, _, err := storeKeys(file, candidates)
	return enrolled, err
}

// storeKeys reads the store's lines whose host field matches a candidate:
// enrolled the keys of plain lines, revoked those of lines marked @revoked.
// A line marked @cert-authority holds neither (the native transport takes
// no certificates, and OpenSSH reads such lines itself), nor does a line
// with any other marker, which OpenSSH reads as invalid.
func storeKeys(file string, candidates []string) (enrolled, revoked []keyRecord, err error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	enrolled, revoked = []keyRecord{}, []keyRecord{}
	seen := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		marker := ""
		if strings.HasPrefix(fields[0], "@") {
			marker, fields = fields[0], fields[1:]
		}
		if marker != "" && marker != "@revoked" {
			continue
		}
		if len(fields) < 3 || !hostFieldMatches(fields[0], candidates) {
			continue
		}
		rec := keyRecord{Type: fields[1], Blob: fields[2]}
		if _, decodeErr := base64.StdEncoding.DecodeString(rec.Blob); decodeErr != nil {
			return nil, nil, errorcodes.Errorf("host_key_trust_store_entry_invalid", "matching known_hosts entry contains invalid key data: %w", decodeErr)
		}
		key := marker + " " + rec.Type + " " + rec.Blob
		if seen[key] {
			continue
		}
		seen[key] = true
		if marker == "@revoked" {
			revoked = append(revoked, rec)
		} else {
			enrolled = append(enrolled, rec)
		}
	}
	return enrolled, revoked, scanner.Err()
}

// revokedError is host_key_revoked for host when any of the presented keys
// is among revoked, naming those keys' fingerprints; nil when none is.
func revokedError(host, file string, presented, revoked []keyRecord) error {
	var hit []keyRecord
	for _, key := range presented {
		if sameKey(revoked, key) {
			hit = append(hit, key)
		}
	}
	if len(hit) == 0 {
		return nil
	}
	return &Error{Code: "host_key_revoked", Host: host, Path: file, Err: fmt.Errorf("presented=%s", strings.Join(fingerprints(hit), ","))}
}

func hostFieldMatches(field string, candidates []string) bool {
	positive := false
	for _, token := range strings.Split(field, ",") {
		negated := strings.HasPrefix(token, "!")
		if negated {
			token = strings.TrimPrefix(token, "!")
		}
		matched := false
		for _, candidate := range candidates {
			if hostTokenMatches(token, candidate) {
				matched = true
				break
			}
		}
		if matched && negated {
			return false
		}
		if matched {
			positive = true
		}
	}
	return positive
}

func hostTokenMatches(token, candidate string) bool {
	if strings.EqualFold(token, candidate) {
		return true
	}
	if strings.HasPrefix(token, "|1|") {
		parts := strings.Split(token, "|")
		if len(parts) != 4 {
			return false
		}
		salt, err1 := base64.StdEncoding.DecodeString(parts[2])
		expected, err2 := base64.StdEncoding.DecodeString(parts[3])
		if err1 != nil || err2 != nil {
			return false
		}
		mac := hmac.New(sha1.New, salt)
		_, _ = mac.Write([]byte(candidate))
		return hmac.Equal(mac.Sum(nil), expected)
	}
	ok, err := path.Match(strings.ToLower(token), strings.ToLower(candidate))
	return err == nil && ok
}

// Identity is the name a device's host key is enrolled and checked under on
// both transports: the canonical name on port
// 22, [canonical]:PORT on any other port, since another port may be another
// SSH server with another key. The system transport passes it to OpenSSH as
// HostKeyAlias.
func Identity(host string, port int) string {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if port == 0 || port == 22 {
		return host
	}
	return "[" + host + "]:" + strconv.Itoa(port)
}

func fingerprints(keys []keyRecord) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		blob, err := base64.StdEncoding.DecodeString(key.Blob)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(blob)
		out = append(out, key.Type+" SHA256:"+base64.RawStdEncoding.EncodeToString(sum[:]))
	}
	sort.Strings(out)
	return out
}

// Enroll safely appends presented keys under the device's identity if it
// remains unknown. It rereads the file while holding an exclusive advisory
// lock, refuses a key the store marks @revoked for the device
// (host_key_revoked), never replaces or rewrites unrelated lines, and ends a
// last line that lacks its newline before appending. wrote is false when
// another process enrolled the same key meanwhile: this call stored nothing.
func Enroll(file, host string, port int, presented []keyRecord) (wrote bool, err error) {
	if len(presented) == 0 {
		return false, &Error{Code: "host_key_enrollment_empty", Host: host, Path: file}
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return false, &Error{Code: "host_key_enrollment_open_failed", Host: host, Path: file, Err: err}
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return false, &Error{Code: "host_key_enrollment_lock_failed", Host: host, Path: file, Err: err}
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	enrolled, revoked, err := storeKeys(file, []string{Identity(host, port)})
	if err != nil {
		return false, &Error{Code: "host_key_enrollment_read_failed", Host: host, Path: file, Err: err}
	}
	if err := revokedError(host, file, presented, revoked); err != nil {
		return false, err
	}
	if len(enrolled) > 0 {
		for _, live := range presented {
			for _, stored := range enrolled {
				if live.Type == stored.Type && live.Blob == stored.Blob {
					return false, nil
				}
			}
		}
		return false, &Error{Code: "host_key_changed_during_enrollment", Host: host, Path: file, Err: errors.New("enrolled key changed during acceptance")}
	}
	ended, err := endsInNewline(f)
	if err != nil {
		return false, &Error{Code: "host_key_enrollment_read_failed", Host: host, Path: file, Err: err}
	}
	hostField := Identity(host, port)
	var b strings.Builder
	if !ended {
		b.WriteByte('\n')
	}
	for _, key := range presented {
		fmt.Fprintf(&b, "%s %s %s karvi-auto-enrolled\n", hostField, key.Type, key.Blob)
	}
	if _, err := f.WriteString(b.String()); err != nil {
		return false, &Error{Code: "host_key_enrollment_write_failed", Host: host, Path: file, Err: err}
	}
	if err := f.Sync(); err != nil {
		return false, &Error{Code: "host_key_enrollment_sync_failed", Host: host, Path: file, Err: err}
	}
	return true, nil
}

// endsInNewline says whether f is empty or its last byte is a newline, so
// that a line appended to it starts a line of its own.
func endsInNewline(f *os.File) (bool, error) {
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return err == nil, err
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, info.Size()-1); err != nil {
		return false, err
	}
	return last[0] == '\n', nil
}
