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

// WarnInsecureSystem emits the mandatory insecure-mode warning and, when an
// enrolled key exists, compares it to the currently presented key so a changed
// key produces a specific warning while the connection remains permitted.
func WarnInsecureSystem(ctx context.Context, p Policy, host, address string, port int, warn func(string)) {
	if p.Mode != Insecure {
		return
	}
	warnInsecureBase(warn, host)
	if _, err := os.Stat(p.KnownHostsFile); err != nil || !HasEnrolledHost(p.KnownHostsFile, host, port) {
		return
	}
	inspection, err := InspectRemote(ctx, p.KnownHostsFile, host, address, port, 5*time.Second)
	if err != nil {
		emit(warn, fmt.Sprintf("WARNING: insecure host-key comparison for %s could not complete: %v; connection remains allowed by policy", host, err))
		return
	}
	if inspection.Comparison == Mismatch {
		emit(warn, mismatchAcceptedWarning(host, inspection))
	}
}

func warnInsecureBase(warn func(string), host string) {
	emit(warn, fmt.Sprintf("WARNING: SSH host-key policy insecure for %s: unknown and changed keys are accepted; this permits machine-in-the-middle impersonation", host))
}

func mismatchDescription(host string, i Inspection) string {
	return fmt.Sprintf("SSH host key mismatch for %s: enrolled=%s presented=%s", host, strings.Join(i.EnrolledFingerprints, ","), strings.Join(i.PresentedFingerprints, ","))
}

func mismatchAcceptedWarning(host string, i Inspection) string {
	return "WARNING: " + mismatchDescription(host, i) + "; mismatch accepted only because policy=insecure"
}

func emit(warn func(string), message string) {
	if warn != nil {
		warn(message)
	}
}

// InspectRemote obtains public host keys with ssh-keyscan and compares them
// numerically/by key blob against matching OpenSSH known_hosts entries.
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
	output, err := cmd.Output()
	presented, parseErr := parseScannedKeys(output)
	if parseErr != nil {
		return Inspection{}, parseErr
	}
	if len(presented) == 0 {
		if err != nil {
			return Inspection{}, fmt.Errorf("ssh-keyscan failed: %s", strings.TrimSpace(stderr.String()))
		}
		return Inspection{}, errorcodes.Errorf("host_key_scan_empty", "ssh-keyscan returned no usable host keys")
	}

	enrolled, readErr := matchingKeys(knownHostsFile, []string{Identity(host, port)})
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return Inspection{}, errorcodes.Ensure(readErr, "host_key_trust_store_unavailable")
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

// HasEnrolledHost reports whether the store holds a key for the device's
// identity on port.
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

func matchingKeys(file string, candidates []string) ([]keyRecord, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	out := []keyRecord{}
	seen := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		offset := 0
		if strings.HasPrefix(fields[0], "@") {
			offset = 1
		}
		if len(fields) < offset+3 || !hostFieldMatches(fields[offset], candidates) {
			continue
		}
		rec := keyRecord{Type: fields[offset+1], Blob: fields[offset+2]}
		if _, decodeErr := base64.StdEncoding.DecodeString(rec.Blob); decodeErr != nil {
			return nil, errorcodes.Errorf("host_key_trust_store_entry_invalid", "matching known_hosts entry contains invalid key data: %w", decodeErr)
		}
		key := rec.Type + " " + rec.Blob
		if !seen[key] {
			seen[key] = true
			out = append(out, rec)
		}
	}
	return out, scanner.Err()
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
// lock and never replaces or rewrites unrelated lines. wrote is false when
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

	enrolled, err := matchingKeys(file, []string{Identity(host, port)})
	if err != nil {
		return false, &Error{Code: "host_key_enrollment_read_failed", Host: host, Path: file, Err: err}
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
	hostField := Identity(host, port)
	var b strings.Builder
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
