// Package transcript implements the login recording destination, layout,
// naming, metadata, and post-processing rules. The script(1) wrapper in the
// CLI drives it; nothing here reads the device stream.
package transcript

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// Formats of transcript.format and transcript.metadata-format.
const (
	FormatText  = "text"
	FormatJSONL = "jsonl"
	FormatJSON  = "json"
)

// TranscriptExtension is the extension of a transcript in the given format.
func TranscriptExtension(format string) string {
	switch format {
	case FormatJSONL:
		return ".jsonl"
	case FormatJSON:
		return ".json"
	}
	return ".log"
}

// MetadataExtension is the extension of a metadata file in the given format.
func MetadataExtension(format string) string {
	switch format {
	case FormatText:
		return ".meta.txt"
	case FormatJSON:
		return ".meta.json"
	}
	return ".meta.jsonl"
}

// SafeName is the <device> part of a transcript name: the
// lowercase target name with every character other than letters, digits,
// ".", "_", and "-" replaced by "_".
func SafeName(target string) string {
	safe := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return '_'
	}, strings.ToLower(target))
	if safe == "" {
		return "device"
	}
	return safe
}

// Destination is where a session records.
type Destination struct {
	Root string // transcript.root or --record=PATH
	Day  string // <Root>/YYYY-MM-DD
}

// Resolve chooses the destination root: record is the --record value ("" for
// the configured root, otherwise PATH), rootSetting is transcript.root,
// shared is sharedroot, and rootLocked reports whether transcript.root is
// locked. A PATH that exists and is not a folder is refused with
// transcript_path_not_directory; PATH with a locked root with
// transcript_root_locked (both exit ExitUsageError).
func Resolve(record, rootSetting, shared, base, home string, rootLocked bool, started time.Time, loc *time.Location) (Destination, error) {
	root := ""
	if record != "" {
		if rootLocked {
			return Destination{}, errorcodes.Errorf("transcript_root_locked", "--record=PATH is refused because transcript.root is locked; record without a path to use %s", rootSetting)
		}
		p, err := expandHome(record, home)
		if err != nil {
			return Destination{}, errorcodes.Errorf("transcript_path_invalid", "resolve transcript path: %w", err)
		}
		p, err = filepath.Abs(p)
		if err != nil {
			return Destination{}, errorcodes.Errorf("transcript_path_invalid", "resolve transcript path: %w", err)
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return Destination{}, errorcodes.Errorf("transcript_path_not_directory", "--record=%s exists and is not a folder; a session records into <PATH>/YYYY-MM-DD/", record)
		}
		root = p
	} else {
		p, err := osutil.ResolveTranscriptRoot(rootSetting, shared, base, home)
		if err != nil {
			return Destination{}, errorcodes.Errorf("transcript_path_invalid", "resolve transcript.root: %w", err)
		}
		root = p
	}
	return Destination{Root: root, Day: filepath.Join(root, osutil.DayFolder(started, loc))}, nil
}

func expandHome(raw, home string) (string, error) {
	if raw == "~" {
		return home, nil
	}
	if strings.HasPrefix(raw, "~/") {
		return filepath.Join(home, raw[2:]), nil
	}
	if strings.HasPrefix(raw, "~") {
		return "", errorcodes.Errorf("path_other_user_home_unsupported", "~otheruser paths are not supported: %s", raw)
	}
	return raw, nil
}

// Pair is a claimed transcript and metadata file sharing one name.
type Pair struct {
	Transcript string
	Metadata   string
}

// Claim creates the destination day folder if needed (mode; under a setgid
// root the root's mode) and claims <device>-<HHMMSS><ext> and
// <device>-<HHMMSS><metaExt> in it, exclusively. When either name is taken,
// both are bumped together with the first free numeric suffix. Both files
// exist, empty and mode 0640, when Claim returns.
func Claim(dest Destination, device string, started time.Time, loc *time.Location, ext, metaExt string, mode os.FileMode) (Pair, error) {
	if err := osutil.EnsureOutputDirectory(dest.Day, osutil.DayFolderMode(dest.Root, mode), "transcript_directory_not_writable"); err != nil {
		return Pair{}, err
	}
	if loc == nil {
		loc = time.Local
	}
	base := SafeName(device) + "-" + started.In(loc).Format("150405")
	for n := 0; n < 10000; n++ {
		name := base
		if n > 0 {
			name = base + "." + strconv.Itoa(n)
		}
		p := Pair{Transcript: filepath.Join(dest.Day, name+ext), Metadata: filepath.Join(dest.Day, name+metaExt)}
		tf, err := osutil.CreateExclusive(p.Transcript, 0)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return Pair{}, errorcodes.Errorf("transcript_create_failed", "create %s: %w", p.Transcript, err)
		}
		tf.Close()
		mf, err := osutil.CreateExclusive(p.Metadata, 0)
		if err != nil {
			os.Remove(p.Transcript)
			if os.IsExist(err) {
				continue
			}
			return Pair{}, errorcodes.Errorf("transcript_create_failed", "create %s: %w", p.Metadata, err)
		}
		mf.Close()
		return p, nil
	}
	return Pair{}, errorcodes.Errorf("transcript_create_failed", "no free transcript name for %s in %s", base, dest.Day)
}

// Metadata is the session metadata record. Start fields
// are written when the session begins; End fields at session end.
type Metadata struct {
	SessionID        string
	OperatorUsername string
	OperatorUID      int
	InputTarget      string
	DeviceName       string
	CanonicalName    string
	Platform         string
	Transport        string
	DispatchOrder    string
	ShuffleKey       *string
	CandidateCount   int
	TranscriptFile   string
	TranscriptFormat string
	Rows, Columns    int
	StartedAt        time.Time
	// End fields.
	EndedAt            time.Time
	ExitClassification string
	TranscriptSHA256   string
	RecordingFailed    bool
}

func (m Metadata) fields(record string) []kv {
	out := []kv{
		{"schema_version", 1},
		{"record", record},
		{"session_id", m.SessionID},
		{"operator", []kv{{"username", m.OperatorUsername}, {"uid", m.OperatorUID}}},
		{"input_target", m.InputTarget},
		{"device", []kv{{"name", m.DeviceName}, {"canonical_name", m.CanonicalName}, {"platform", m.Platform}}},
		{"transport", m.Transport},
		{"dispatch_order", m.DispatchOrder},
	}
	if m.ShuffleKey != nil {
		out = append(out, kv{"shuffle_key", *m.ShuffleKey})
	}
	out = append(out,
		kv{"candidate_count", m.CandidateCount},
		kv{"transcript_file", m.TranscriptFile},
		kv{"transcript_format", m.TranscriptFormat},
		kv{"terminal", []kv{{"rows", m.Rows}, {"columns", m.Columns}}},
		kv{"started_at", m.StartedAt.Format(time.RFC3339Nano)},
	)
	if record == "end" {
		out = append(out,
			kv{"ended_at", m.EndedAt.Format(time.RFC3339Nano)},
			kv{"exit_classification", m.ExitClassification},
			kv{"transcript_sha256", m.TranscriptSHA256},
			kv{"recording_failed", m.RecordingFailed},
		)
	}
	return out
}

// kv is one metadata field; a nested []kv is an object whose fields keep
// their order in every format.
type kv struct {
	key   string
	value any
}

func jsonObject(fields []kv) ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(f.key)
		var v []byte
		var err error
		if nested, ok := f.value.([]kv); ok {
			v, err = jsonObject(nested)
		} else {
			v, err = json.Marshal(f.value)
		}
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func textLines(fields []kv) []byte {
	var b bytes.Buffer
	for _, f := range fields {
		if nested, ok := f.value.([]kv); ok {
			for _, n := range nested {
				fmt.Fprintf(&b, "%s.%s: %v\n", f.key, n.key, n.value)
			}
			continue
		}
		fmt.Fprintf(&b, "%s: %v\n", f.key, f.value)
	}
	return b.Bytes()
}

// WriteStart writes the start record: a jsonl line or the text
// fields appended to the claimed metadata file. A json metadata file holds
// only the end document, so nothing is written yet.
func WriteStart(path, format string, m Metadata) error {
	switch format {
	case FormatJSON:
		return nil
	case FormatText:
		return appendBytes(path, textLines(m.fields("start")))
	}
	line, err := jsonObject(m.fields("start"))
	if err != nil {
		return err
	}
	return appendBytes(path, append(line, '\n'))
}

// WriteEnd writes the end record: a jsonl end line, the end fields appended
// to a text file, or the whole json document by atomic replacement.
func WriteEnd(path, format string, m Metadata) error {
	switch format {
	case FormatJSON:
		doc, err := jsonObject(m.fields("end"))
		if err != nil {
			return err
		}
		return replaceBytes(path, append(doc, '\n'))
	case FormatText:
		end := m.fields("end")
		return appendBytes(path, textLines(end[len(m.fields("start")):]))
	}
	line, err := jsonObject(m.fields("end"))
	if err != nil {
		return err
	}
	return appendBytes(path, append(line, '\n'))
}

func appendBytes(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

// replaceBytes writes data to path by exclusive temporary file and rename in
// the same folder.
func replaceBytes(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".karvi-transcript-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		tmp.Close()
		if !ok {
			os.Remove(name)
		}
	}()
	if err := tmp.Chmod(osutil.OutputFileMode); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}

// StripScriptMarkers removes the "Script started on ..." first line and the
// "Script done on ..." last line that util-linux script(1) writes into the
// file, so the transcript holds only the device stream. The file is
// rewritten by atomic replacement. A session killed before
// this runs keeps the marker lines.
func StripScriptMarkers(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out := data
	if i := bytes.IndexByte(out, '\n'); bytes.HasPrefix(out, []byte("Script started on ")) {
		if i < 0 {
			out = nil
		} else {
			out = out[i+1:]
		}
	}
	trimmed := bytes.TrimRight(out, "\r\n")
	if j := bytes.LastIndexByte(trimmed, '\n'); true {
		last := trimmed
		if j >= 0 {
			last = trimmed[j+1:]
		}
		// script(1) writes "\n" before its trailer; that newline goes with it.
		if bytes.HasPrefix(bytes.TrimLeft(last, "\r"), []byte("Script done on ")) {
			if j < 0 {
				out = nil
			} else {
				out = out[:j]
			}
		}
	}
	if bytes.Equal(out, data) {
		return nil
	}
	return replaceBytes(path, out)
}

// FileSHA256 is the lowercase hexadecimal digest of a file.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// IsEnd reports whether a metadata file holds its end record: a jsonl line
// with "record":"end", a non-empty json document, or a text ended_at line.
func IsEnd(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	switch {
	case strings.HasSuffix(path, ".meta.json"):
		var m map[string]any
		return json.Unmarshal(data, &m) == nil && m["record"] == "end"
	case strings.HasSuffix(path, ".meta.txt"):
		return bytes.Contains(data, []byte("\nended_at: ")) || bytes.HasPrefix(data, []byte("ended_at: "))
	}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		var m map[string]any
		if json.Unmarshal(line, &m) == nil && m["record"] == "end" {
			return true
		}
	}
	return false
}

// EndedAt returns the ended_at time of a metadata file when present.
func EndedAt(path string) (time.Time, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, false
	}
	if strings.HasSuffix(path, ".meta.txt") {
		for _, line := range strings.Split(string(data), "\n") {
			if v, ok := strings.CutPrefix(line, "ended_at: "); ok {
				t, err := time.Parse(time.RFC3339Nano, v)
				return t, err == nil
			}
		}
		return time.Time{}, false
	}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		var m struct {
			Record  string `json:"record"`
			EndedAt string `json:"ended_at"`
		}
		if json.Unmarshal(line, &m) == nil && m.Record == "end" {
			t, err := time.Parse(time.RFC3339Nano, m.EndedAt)
			return t, err == nil
		}
	}
	return time.Time{}, false
}

// TranscriptFor returns the transcript that shares a metadata file's name:
// <stem>.meta.<ext> pairs with <stem>.log, .jsonl, or .json.
func TranscriptFor(metaPath string) (string, bool) {
	name := filepath.Base(metaPath)
	i := strings.Index(name, ".meta.")
	if i < 0 {
		return "", false
	}
	stem := filepath.Join(filepath.Dir(metaPath), name[:i])
	for _, ext := range []string{".log", ".jsonl", ".json"} {
		if _, err := os.Lstat(stem + ext); err == nil {
			return stem + ext, true
		}
	}
	return "", false
}
