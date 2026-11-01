// Package transcript implements the login recording destination, layout,
// naming, metadata, and post-processing rules. The script(1) wrapper in the
// CLI drives it; the device stream is read only at the session's end, to
// render it.
package transcript

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/termtext"
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
	Root   string // transcript.root or --record=PATH
	Folder string // <Root>/YYMMDD, or PATH itself
	Chosen bool   // --record=PATH: the operator's folder, the device's name
}

// Resolve chooses the destination root: record is the --record value ("" for
// the configured root, otherwise PATH), rootSetting is transcript.root,
// shared is sharedroot, and rootLocked reports whether transcript.root is
// locked. The configured root records into its day folder, PATH into
// itself. A PATH that exists and is not a folder is refused with
// transcript_path_not_directory; PATH with a locked root with
// transcript_root_locked (both exit ExitUsageError).
func Resolve(record, rootSetting, shared, base, home string, rootLocked bool, started time.Time, loc *time.Location) (Destination, error) {
	if record != "" {
		if rootLocked {
			return Destination{}, errorcodes.Errorf("transcript_root_locked", "--record=PATH is refused because transcript.root is locked; record without a path to use %s", rootSetting)
		}
		p, err := osutil.ResolvePath(record, home)
		if err != nil {
			return Destination{}, errorcodes.Errorf("transcript_path_invalid", "resolve transcript path: %w", err)
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return Destination{}, errorcodes.Errorf("transcript_path_not_directory", "--record=%s; name a folder", record)
		}
		return Destination{Root: p, Folder: p, Chosen: true}, nil
	}
	root, err := osutil.ResolveTranscriptRoot(rootSetting, shared, base, home)
	if err != nil {
		return Destination{}, errorcodes.Errorf("transcript_path_invalid", "resolve transcript.root: %w", err)
	}
	return Destination{Root: root, Folder: filepath.Join(root, osutil.DayFolder(started, loc))}, nil
}

// Pair is a claimed transcript and metadata file sharing one name. A day
// folder of a shared tree is the group's to write, with no sticky bit, so
// another member can rename a session's files and put a link or a file of
// its own in their place: the pair holds the files Claim created, every
// later open goes by the name without following a link, and a name that no
// longer holds the pair's file is refused with transcript_create_failed
// (ErrReplaced), nothing read or written through it. The transcript stays
// open from the claim for the recorder (RecorderPath) until Close.
type Pair struct {
	Transcript string
	Metadata   string
	// transcript and metadata are the files as Claim created them, or as the
	// end's atomic replacement wrote them.
	transcript, metadata os.FileInfo
	recorder             *os.File
}

// ErrReplaced is a claimed name that no longer holds the session's file.
var ErrReplaced = errorcodes.Errorf("transcript_create_failed", "not the session's file")

// RecorderPath names the claimed transcript for script(1), which opens its
// file by name: the wrapper's own descriptor under /proc, which reaches the
// file Claim created whatever the folder's name holds by then.
func (p *Pair) RecorderPath() string {
	return "/proc/" + strconv.Itoa(os.Getpid()) + "/fd/" + strconv.FormatUint(uint64(p.recorder.Fd()), 10)
}

// Close releases the transcript held for the recorder.
func (p *Pair) Close() {
	if p.recorder != nil {
		p.recorder.Close()
	}
}

// replaced is the refusal of a name that no longer holds the pair's file.
func replaced(path string) error {
	return fmt.Errorf("%s is %w", path, ErrReplaced)
}

// openHeld opens path, which held is the pair's file at, by its name: never
// through a link, never waiting on a FIFO put there, and refused when the
// file opened is not the one held.
func openHeld(path string, flag int, held os.FileInfo) (*os.File, error) {
	f, err := os.OpenFile(path, flag|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ELOOP) {
		return nil, replaced(path)
	}
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err == nil && !isHeld(held, fi) {
		err = replaced(path)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// checkHeld refuses a path whose name no longer holds the pair's file.
func checkHeld(path string, held os.FileInfo) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !isHeld(held, fi) {
		return replaced(path)
	}
	return nil
}

// isHeld reports whether fi is the held file: the same device and inode, a
// regular file, and the same owner, so an inode another member's new file
// reuses is not taken for the session's.
func isHeld(held, fi os.FileInfo) bool {
	if held == nil || !os.SameFile(held, fi) || !fi.Mode().IsRegular() {
		return false
	}
	a, ok := held.Sys().(*syscall.Stat_t)
	b, ok2 := fi.Sys().(*syscall.Stat_t)
	return ok && ok2 && a.Uid == b.Uid
}

// Claim creates the destination folder if needed (mode; a day folder under a
// setgid root takes the root's mode) and claims a transcript and its
// metadata under one name, <name><ext> and <name><metaExt>, exclusively.
// When either name is taken, both move to the next name together. In the
// configured root the names are <device>-<HHMMSS>, then .1, .2, and on; in
// --record=PATH they are <device>, then <device>-<YYMMDD>-<HHMMSS>, and with
// both taken the claim is refused with transcript_name_taken. Both files
// exist, empty and mode 0640, when Claim returns; the pair holds them
// (Pair), and its caller closes it.
func Claim(dest Destination, device string, started time.Time, loc *time.Location, ext, metaExt string, mode os.FileMode) (Pair, error) {
	if err := osutil.EnsureOutputDirectory(dest.Folder, osutil.DayFolderMode(dest.Root, mode), "transcript_directory_not_writable"); err != nil {
		return Pair{}, err
	}
	if loc == nil {
		loc = time.Local
	}
	safe := SafeName(device)
	base := safe + "-" + started.In(loc).Format("150405")
	// nth is the claim's nth name, false past the last.
	nth := func(n int) (string, bool) {
		switch {
		case dest.Chosen && n == 0:
			return safe, true
		case dest.Chosen && n == 1:
			return safe + "-" + started.In(loc).Format("060102-150405"), true
		case dest.Chosen:
			return "", false
		case n == 0:
			return base, true
		}
		return base + "." + strconv.Itoa(n), n < 10000
	}
	var taken []string
	for n := 0; ; n++ {
		name, more := nth(n)
		if !more {
			break
		}
		p := Pair{Transcript: filepath.Join(dest.Folder, name+ext), Metadata: filepath.Join(dest.Folder, name+metaExt)}
		tf, err := osutil.CreateExclusive(p.Transcript, 0)
		if err != nil {
			if os.IsExist(err) {
				taken = append(taken, filepath.Base(p.Transcript))
				continue
			}
			return Pair{}, errorcodes.Errorf("transcript_create_failed", "create %s: %w", p.Transcript, err)
		}
		if p.transcript, err = tf.Stat(); err != nil {
			tf.Close()
			os.Remove(p.Transcript)
			return Pair{}, errorcodes.Errorf("transcript_create_failed", "create %s: %w", p.Transcript, err)
		}
		mf, err := osutil.CreateExclusive(p.Metadata, 0)
		if err != nil {
			tf.Close()
			os.Remove(p.Transcript)
			if os.IsExist(err) {
				taken = append(taken, filepath.Base(p.Metadata))
				continue
			}
			return Pair{}, errorcodes.Errorf("transcript_create_failed", "create %s: %w", p.Metadata, err)
		}
		p.metadata, err = mf.Stat()
		mf.Close()
		if err != nil {
			tf.Close()
			os.Remove(p.Transcript)
			os.Remove(p.Metadata)
			return Pair{}, errorcodes.Errorf("transcript_create_failed", "create %s: %w", p.Metadata, err)
		}
		p.recorder = tf
		return p, nil
	}
	if dest.Chosen {
		return Pair{}, errorcodes.Errorf("transcript_name_taken", "%s; retry\n  %s", dest.Folder, strings.Join(taken, "\n  "))
	}
	return Pair{}, errorcodes.Errorf("transcript_create_failed", "no free transcript name for %s in %s", base, dest.Folder)
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
func (p *Pair) WriteStart(format string, m Metadata) error {
	switch format {
	case FormatJSON:
		return nil
	case FormatText:
		return appendBytes(p.Metadata, p.metadata, textLines(m.fields("start")))
	}
	line, err := jsonObject(m.fields("start"))
	if err != nil {
		return err
	}
	return appendBytes(p.Metadata, p.metadata, append(line, '\n'))
}

// WriteEnd writes the end record: a jsonl end line, the end fields appended
// to a text file, or the whole json document by atomic replacement.
func (p *Pair) WriteEnd(format string, m Metadata) error {
	switch format {
	case FormatJSON:
		doc, err := jsonObject(m.fields("end"))
		if err != nil {
			return err
		}
		if err := checkHeld(p.Metadata, p.metadata); err != nil {
			return err
		}
		fi, err := replaceBytes(p.Metadata, append(doc, '\n'))
		if err == nil {
			p.metadata = fi
		}
		return err
	case FormatText:
		end := m.fields("end")
		return appendBytes(p.Metadata, p.metadata, textLines(end[len(m.fields("start")):]))
	}
	line, err := jsonObject(m.fields("end"))
	if err != nil {
		return err
	}
	return appendBytes(p.Metadata, p.metadata, append(line, '\n'))
}

// appendBytes appends data to the held file at path.
func appendBytes(path string, held os.FileInfo, data []byte) error {
	f, err := openHeld(path, os.O_WRONLY|os.O_APPEND, held)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

// replaceBytes writes data to path by exclusive temporary file and rename in
// the same folder, and returns the file written.
func replaceBytes(path string, data []byte) (os.FileInfo, error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".karvi-transcript-")
	if err != nil {
		return nil, err
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
		return nil, err
	}
	if _, err := tmp.Write(data); err != nil {
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		return nil, err
	}
	fi, err := tmp.Stat()
	if err != nil {
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(name, path); err != nil {
		return nil, err
	}
	ok = true
	return fi, nil
}

// Render rewrites a finished session's transcript as the operator saw it:
// the "Script started on ..." first line and the "Script done on ..." last
// line that util-linux script(1) writes into the file are removed, and the
// device's bytes between them are rendered (termtext) at the terminal's
// columns and each resize from script(1)'s timing log. The file is rewritten
// by atomic replacement, and no raw copy is kept; a session killed before
// this runs keeps its bytes as script(1) wrote them.
func (p *Pair) Render(columns int, resizes []termtext.Resize) error {
	f, err := openHeld(p.Transcript, os.O_RDONLY, p.transcript)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		return err
	}
	out := termtext.Render(stripMarkers(data), columns, resizes)
	if bytes.Equal(out, data) {
		return nil
	}
	fi, err := replaceBytes(p.Transcript, out)
	if err == nil {
		p.transcript = fi
	}
	return err
}

// ReadTiming is the starting columns and the resizes in script(1)'s advanced
// timing log at path (termtext.ParseTiming).
func ReadTiming(path string) (int, []termtext.Resize, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()
	return termtext.ParseTiming(f)
}

// stripMarkers is data without script(1)'s first and last lines, which
// leaves the bytes the timing log's output counts measure.
func stripMarkers(data []byte) []byte {
	out := data
	if i := bytes.IndexByte(out, '\n'); bytes.HasPrefix(out, []byte("Script started on ")) {
		if i < 0 {
			out = nil
		} else {
			out = out[i+1:]
		}
	}
	trimmed := bytes.TrimRight(out, "\r\n")
	last := trimmed
	j := bytes.LastIndexByte(trimmed, '\n')
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
	return out
}

// TranscriptSHA256 is the lowercase hexadecimal digest of the transcript.
func (p *Pair) TranscriptSHA256() (string, error) {
	f, err := openHeld(p.Transcript, os.O_RDONLY, p.transcript)
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

// metadataStem is name without its metadata extension (MetadataExtension's
// values), false when name does not end in one. The stem is matched at the
// name's end, since a device's name may hold ".meta." itself.
func metadataStem(name string) (string, bool) {
	for _, format := range []string{FormatJSONL, FormatJSON, FormatText} {
		if stem, ok := strings.CutSuffix(name, MetadataExtension(format)); ok && stem != "" {
			return stem, true
		}
	}
	return "", false
}

// IsMetadata reports whether a file name is a metadata file's.
func IsMetadata(name string) bool {
	_, ok := metadataStem(name)
	return ok
}

// TranscriptFor returns the transcript that shares a metadata file's name:
// <stem>.meta.<ext> pairs with <stem>.log, .jsonl, or .json.
func TranscriptFor(metaPath string) (string, bool) {
	name, ok := metadataStem(filepath.Base(metaPath))
	if !ok {
		return "", false
	}
	stem := filepath.Join(filepath.Dir(metaPath), name)
	for _, ext := range []string{".log", ".jsonl", ".json"} {
		if _, err := os.Lstat(stem + ext); err == nil {
			return stem + ext, true
		}
	}
	return "", false
}
