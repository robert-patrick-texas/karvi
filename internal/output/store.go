// Package output owns durable activity artifacts. Command JSONL is append-only;
// summaries and metrics are same-directory temp-write, fsync, and rename.
package output

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/platform"
	"github.com/robert-patrick-texas/karvi/records"
)

// Paths names the job folder and the files in it that this job writes: the
// path of a skipped file is empty, so that a reader (the summary's "paths")
// is told of no file that will not exist. With every file skipped Root is
// empty too.
type Paths struct{ Root, Manifest, CommandsJSONL, CommandsText, FailuresJSONL, FailedDevices, Metrics, Summary string }

// FileSet names the job's output files, one field per file, shaped like
// the file names. OutputTxt is every
// device's output.TARGET.txt.
type FileSet struct {
	CommandsJSONL, CommandsTxt, FailedDevicesTxt, FailuresJSONL, ManifestJSON, MetricsJSON, SummaryJSON, OutputTxt bool
}

// AllFiles is every output file: as Options.Skip, a store that writes
// nothing and creates no job folder (`cmd --nof`).
var AllFiles = FileSet{true, true, true, true, true, true, true, true}

type Options struct {
	// Skip is the files this job does not write; the zero value writes
	// them all. A skipped file is not created. The store is otherwise the
	// same: records are validated and given their sequence, so the caller
	// renders and counts as it does with files. With every file skipped no
	// folder is created and the space preflight is not run.
	//
	// MaxJobBytes limits what the store appends for the devices' output:
	// the commands.jsonl lines and the text blocks together, each only if
	// its file is written. The admission's free-space
	// check (preflight.go) counts the same copies of the device estimate.
	Skip      FileSet
	Root, ID  string
	CropNames bool // output.crop-to-dot from the plan: a device name in a file name is its first label
	// Collection is present for a crun: the store then writes one file per
	// device into Directory, whatever Skip says.
	Collection    *CollectionOptions
	DirectoryMode os.FileMode // output.directory-mode for folders karvi creates; zero means the default
	Fsync         bool
	MaxJobBytes   int64
	// Warn receives the notice of a device's text file that could not be
	// written (output_text_write_failed); nil discards it.
	Warn func(string)
	// Timestamp writes the text file's header time (display.timestamp in
	// the effective timezone, the display Formatter's method); nil is
	// DefaultTimestamp.
	Timestamp Timestamp
	// OnDurable receives each record with its notice once its
	// commands.jsonl line is written, under the store's lock, so that
	// records leave in sequence: the daemon's followers refuse a stream
	// whose sequence does not follow (ipc_result_malformed). It must not
	// block, and it must not keep the record beyond the follower's need:
	// the daemon sends the record itself over the socket and holds it only
	// until every follower has written it.
	// It was called by the executor after AppendRecord returned, which let
	// one record's notice overtake another's; that was a narrow window
	// until the text block was written in between, when at width 32 every
	// run failed. The follower needs no
	// file, so it need not wait for the text.
	OnDurable func(*records.CommandRecord, Notice)
}

// Notice is what the store knows of a record once it is durable: its
// sequence and ID, and the length of its line with the LF, measured
// whether or not commands.jsonl is written, because the daemon bounds a
// follow frame by it.
//
// LineOffset is where the line begins in commands.jsonl, so a follower is
// fed from the file at the offset and the queue carries no output; -1 when
// the file is not written.
type Notice struct {
	Sequence   int64  `json:"sequence"`
	LineLength int64  `json:"line_length"`
	LineOffset int64  `json:"line_offset"`
	RecordID   string `json:"record_id"`
}
type Store struct {
	mu                 sync.Mutex
	skip               FileSet
	crop               bool // CropNames
	id                 string
	paths              Paths
	commands, failures *os.File
	fsync              bool
	sequence           int64
	bytes              int64 // commands.jsonl, what Bytes reports
	textBytes          int64 // every output.TARGET.txt; counted with bytes against maxBytes
	maxBytes           int64
	stamp              Timestamp // the text files' header time
	// err is the first record that could not be appended (Err).
	err    error
	failed map[string]bool
	// texts is each device's output.TARGET.txt: absent until the device's
	// first record, which creates the file and writes the header; failed
	// after a block could not be written, when the file is left alone.
	texts map[string]textState
	// setups is each device's set-up lines (SetTextSetup), held from the
	// prepared session until the device's first record creates the file.
	setups map[string][]platform.SetupLine
	warn   func(string)
	// onDurable is Options.OnDurable.
	onDurable func(*records.CommandRecord, Notice)
	closed    bool
	// The collection (Options.Collection): each device's temporary while
	// its records arrive, and each ended device's outcome.
	collection  *CollectionOptions
	filters     map[string][]*regexp.Regexp // the plan's crun-filters per platform, compiled (12.19)
	collections map[string]*collectionFile
	outcomes    map[string]records.CollectionDevice
}

// CollectionOptions is where and how a crun's files are written: the
// resolved crun.directory and crun.file-mode, from the plan.
type CollectionOptions struct {
	Directory string
	FileMode  os.FileMode
	// Filters is the plan's crun-filters per platform (12.19), compiled by
	// Create; a platform absent from the map has none.
	Filters map[string][]string
}

// collectionFile is one device's collection in progress: the hidden
// temporary .NAME.JOBID (nil until the device's first written block), how
// many blocks it holds (a blank line goes before every marker but the
// first), and whether a record has made the device a failure, after which
// nothing more is written and the temporary is removed at the device's
// end (12.3 rules 1 and 4).
type collectionFile struct {
	f      *os.File
	w      *bufio.Writer
	tmp    string
	blocks int
	failed bool
}

type textState int

const (
	textAbsent textState = iota
	textOpen
	textFailed
)

// OutputCopies is how many copies of a device's output the root takes: one
// for commands.jsonl and one for its text file, each only if written; the
// admission's estimate counts the same copies the job's byte limit does.
func (f FileSet) OutputCopies() int {
	copies := 0
	if !f.CommandsJSONL {
		copies++
	}
	if !f.OutputTxt {
		copies++
	}
	return copies
}

func Create(opts Options) (*Store, error) {
	var filters map[string][]*regexp.Regexp
	if opts.Collection != nil {
		var err error
		if filters, err = compileCollectionFilters(opts.Collection.Filters); err != nil {
			return nil, err
		}
	}
	if opts.Timestamp == nil {
		opts.Timestamp = DefaultTimestamp()
	}
	if opts.Skip == AllFiles {
		// Nothing is written: no root is needed, no folder is created, no
		// space is asked for, and Paths names no file.
		if opts.ID == "" {
			return nil, errorcodes.Errorf("output_store_options_invalid", "output id is required")
		}
		return &Store{skip: opts.Skip, crop: opts.CropNames, collection: opts.Collection, filters: filters, collections: map[string]*collectionFile{}, outcomes: map[string]records.CollectionDevice{}, failed: map[string]bool{}, texts: map[string]textState{}, setups: map[string][]platform.SetupLine{}, warn: opts.Warn, onDurable: opts.OnDurable, stamp: opts.Timestamp}, nil
	}
	if opts.Root == "" || opts.ID == "" {
		return nil, errorcodes.Errorf("output_store_options_invalid", "output root and id are required")
	}
	// file is the path of a file this job writes, and empty for a skipped one.
	file := func(skipped bool, name string) string {
		if skipped {
			return ""
		}
		return filepath.Join(opts.Root, name)
	}
	skip := opts.Skip
	p := Paths{Root: opts.Root, Manifest: file(skip.ManifestJSON, "manifest.json"), CommandsJSONL: file(skip.CommandsJSONL, "commands.jsonl"), CommandsText: file(skip.CommandsTxt, "commands.txt"), FailuresJSONL: file(skip.FailuresJSONL, "failures.jsonl"), FailedDevices: file(skip.FailedDevicesTxt, "failed-devices.txt"), Metrics: file(skip.MetricsJSON, "metrics.json"), Summary: file(skip.SummaryJSON, "summary.json")}
	s := &Store{skip: opts.Skip, crop: opts.CropNames, collection: opts.Collection, filters: filters, collections: map[string]*collectionFile{}, outcomes: map[string]records.CollectionDevice{}, id: opts.ID, paths: p, fsync: opts.Fsync, maxBytes: opts.MaxJobBytes, failed: map[string]bool{}, texts: map[string]textState{}, setups: map[string][]platform.SetupLine{}, warn: opts.Warn, onDurable: opts.OnDurable, stamp: opts.Timestamp}
	mode := opts.DirectoryMode
	if mode == 0 {
		mode = osutil.DefaultDirectoryMode
	}
	if err := osutil.EnsureOutputDirectory(opts.Root, mode, "output_directory_not_writable"); err != nil {
		return nil, err
	}
	// A skipped .jsonl file is a nil *os.File, which appendLine passes by.
	var err error
	if !opts.Skip.CommandsJSONL {
		if s.commands, err = osutil.CreateExclusive(p.CommandsJSONL, os.O_APPEND); err != nil {
			return nil, err
		}
	}
	if !opts.Skip.FailuresJSONL {
		if s.failures, err = osutil.CreateExclusive(p.FailuresJSONL, os.O_APPEND); err != nil {
			if s.commands != nil {
				s.commands.Close()
			}
			return nil, err
		}
	}
	return s, nil
}
func (s *Store) Paths() Paths { return s.paths }
func (s *Store) WriteCommands(commands []string) error {
	if s.skip.CommandsTxt || len(commands) == 0 {
		return nil
	}
	return atomicBytes(s.paths.CommandsText, commandsText(commands), osutil.OutputFileMode)
}

// WriteCommandLists writes the plan's list per platform as
// commands.PLATFORM.txt beside commands.txt, each a --cf file for that
// platform; nothing when the plan carries no
// list, and nothing when commands.txt is switched off.
func (s *Store) WriteCommandLists(lists map[string][]string) error {
	if s.skip.CommandsTxt {
		return nil
	}
	for name, commands := range lists {
		path := filepath.Join(filepath.Dir(s.paths.CommandsText), "commands."+name+".txt")
		if err := atomicBytes(path, commandsText(commands), osutil.OutputFileMode); err != nil {
			return err
		}
	}
	return nil
}

// commandsText is a command file's content: one statement per line.
func commandsText(commands []string) []byte {
	var b strings.Builder
	for _, c := range commands {
		b.WriteString(c)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}
func (s *Store) WriteManifest(v any) error {
	if s.skip.ManifestJSON {
		return nil
	}
	return osutil.AtomicJSON(s.paths.Manifest, v, osutil.OutputFileMode)
}
func (s *Store) WriteSummary(v any) error {
	if s.skip.SummaryJSON {
		return nil
	}
	return osutil.AtomicJSON(s.paths.Summary, v, osutil.OutputFileMode)
}
func (s *Store) WriteMetrics(v any) error {
	if s.skip.MetricsJSON {
		return nil
	}
	return osutil.AtomicJSON(s.paths.Metrics, v, osutil.OutputFileMode)
}

// AppendRecord appends r's line to commands.jsonl (and failures.jsonl), then
// r's block to its device's output.TARGET.txt: "a statement's block is
// appended when its record is appended".
//
// The record is the authority and the text is derivable from it
// (tools/textfile), so a block that cannot be written does not fail the
// append or the job: the
// device's file is left as it is from then on, so that it ends early
// rather than has a gap, and the notice output_text_write_failed is given
// once for the device.
//
// src is r's output where it is: the record's
// string, or the command's spool file, which every consumer here streams
// from and which the executor removes once AppendRecord and afterRecord
// have returned. A spooled record's Output is
// empty on the way in; the line, the failures line, the text block, and
// the collection block are written from the file, its digest checked on
// the measuring pass first (4.3).
func (s *Store) AppendRecord(r *records.CommandRecord, src Source) (Notice, error) {
	notice, text, err := s.appendLine(r, src)
	if err != nil {
		s.appendFailed(r, err)
		return Notice{}, err
	}
	if s.collection != nil {
		s.appendCollection(r, src)
	}
	if text == textFailed || s.skip.OutputTxt {
		return notice, nil
	}
	var setup []platform.SetupLine
	if text == textAbsent {
		setup = s.takeTextSetup(r.Device.CanonicalName)
	}
	if err := s.appendText(r, src, text == textAbsent, setup); err != nil {
		s.mu.Lock()
		s.texts[r.Device.CanonicalName] = textFailed
		s.mu.Unlock()
		if s.warn != nil {
			// Without commands.jsonl there is nothing to derive the text from.
			remedy := "the device's text can be derived from commands.jsonl"
			if s.skip.CommandsJSONL {
				remedy = "output.files.commands-jsonl is false, so the device's text from there on is not kept"
			}
			s.warn(fmt.Sprintf("output_text_write_failed: %s is not written from record %d on: %v; the job continues, and %s", TextFileName(r.Device.CanonicalName, s.crop), r.Sequence, err, remedy))
		}
	}
	return notice, nil
}

// appendFailed keeps the first record that could not be appended, and lists
// its device for a rerun. The executor fails the device with the error's
// code, but a device's result reaches no operator by itself: through
// v0.12.1 a job that met output.max-job-bytes ended "errored" with the code
// nowhere. The job reads Err once the
// devices are done and ends as an output failure.
func (s *Store) appendFailed(r *records.CommandRecord, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = fmt.Errorf("%w (device %s, command %q)", err, r.Device.CanonicalName, r.Command)
	}
	if r.CommandKind != "session_init" {
		s.failed[r.Device.CanonicalName] = true
	}
}

// Err is the first record that could not be appended, nil if every one was.
func (s *Store) Err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.err }

// SetTextSetup hands the store a device's set-up lines (enable and the
// paging commands, as its session saw them) before the device's first
// record. They have no record, so the executor gives them here once the
// session is prepared, or has failed to prepare. The store keeps them until
// the first record, whose fields make the header they follow: a device has
// a first record on every path, a failed set-up included, and one place
// then creates the file. A device's later
// session in the same job (none today) would not be shown: the lines of a
// device whose file exists are dropped.
func (s *Store) SetTextSetup(target string, lines []platform.SetupLine) {
	if len(lines) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.texts[target] == textAbsent {
		s.setups[target] = lines
	}
}

// takeTextSetup gives up the device's held set-up lines, once.
func (s *Store) takeTextSetup(target string) []platform.SetupLine {
	s.mu.Lock()
	defer s.mu.Unlock()
	lines := s.setups[target]
	delete(s.setups, target)
	return lines
}

// appendText writes r's block, after the header and the device's set-up
// lines when r is its device's first record. It runs outside the store's lock: the file is the device's
// own and a device's records arrive one after another, while at width the
// lock is what every device waits at with its response in hand. The file
// is opened for the block and closed after it, so a job
// holds no descriptor per device between statements, whatever its width,
// and the store needs no word of when a device is done.
func (s *Store) appendText(r *records.CommandRecord, src Source, first bool, setup []platform.SetupLine) error {
	if err := s.reserveText(r, src, first, setup); err != nil {
		return err
	}
	path := filepath.Join(s.paths.Root, TextFileName(r.Device.CanonicalName, s.crop))
	var f *os.File
	var err error
	if first {
		f, err = osutil.CreateExclusive(path, os.O_APPEND)
	} else {
		f, err = os.OpenFile(path, os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW, 0)
	}
	if err != nil {
		return err
	}
	// A small buffer joins the block's short lines into one write; a large
	// output passes through it in pieces, never as a second copy.
	w := bufio.NewWriter(f)
	err = writeText(w, r, src, first, setup, s.stamp)
	if err == nil {
		err = w.Flush()
	}
	if err == nil && s.fsync {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

// writeText is what appendText writes for r: the header and the set-up
// lines before a device's first block, then the block.
func writeText(w io.Writer, r *records.CommandRecord, src Source, first bool, setup []platform.SetupLine, stamp Timestamp) error {
	if first {
		if err := WriteTextHeader(w, r, stamp); err != nil {
			return err
		}
		if err := WriteTextSetup(w, setup); err != nil {
			return err
		}
	}
	return WriteTextBlock(w, r, src)
}

// countingWriter counts what is written to it and keeps nothing.
type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// reserveText counts r's text against output.max-job-bytes before any of it
// is written, as appendCommandsLine does for the line: the text is a second
// copy of the output on disk and the limit is the job's. The size is taken
// by writing the text to a counter, so no copy of the
// output is made. A block that would pass the limit is not written, which
// the caller treats as any text that could not be written: the record is
// kept, the device's file ends there, and the job goes on.
func (s *Store) reserveText(r *records.CommandRecord, src Source, first bool, setup []platform.SetupLine) error {
	if s.maxBytes <= 0 {
		return nil
	}
	var size countingWriter
	if err := writeText(&size, r, src, first, setup, s.stamp); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bytes+s.textBytes+size.n > s.maxBytes {
		return jobLimitExceeded(s.maxBytes)
	}
	s.textBytes += size.n
	return nil
}

// jobLimitExceeded is the one wording of the limit, for a line and for a
// text block.
func jobLimitExceeded(limit int64) error {
	return errorcodes.Errorf("output_job_limit_exceeded", "the job's output would pass output.max-job-bytes (%d bytes)", limit)
}

// appendLine is AppendRecord's part under the lock: the sequence, the
// line, and the state of the device's text file before this record.
func (s *Store) appendLine(r *records.CommandRecord, src Source) (notice Notice, text textState, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Notice{}, textAbsent, errorcodes.Errorf("output_store_closed", "output store is closed")
	}
	s.sequence++
	r.Sequence = s.sequence
	if err := r.Validate(); err != nil {
		return Notice{}, textAbsent, err
	}
	// The line is made and measured whether or not a file takes it: the
	// notice carries its length for the follow frame's bound, and the pass
	// verifies a spool's bytes against the reader's digest before any byte
	// of the line reaches a file.
	line, err := newRecordLine(r, src)
	if err != nil {
		return Notice{}, textAbsent, err
	}
	length, err := line.measure()
	if err != nil {
		return Notice{}, textAbsent, err
	}
	notice = Notice{Sequence: r.Sequence, LineLength: length, LineOffset: -1, RecordID: r.RecordID}
	failed := r.Status != "succeeded"
	if s.commands != nil {
		notice.LineOffset = s.bytes
		if err := s.appendCommandsLine(line, length); err != nil {
			return Notice{}, textAbsent, err
		}
	}
	if failed && s.failures != nil {
		if _, err := writeLine(s.failures, line); err != nil {
			return Notice{}, textAbsent, err
		}
		if s.fsync {
			if err := s.failures.Sync(); err != nil {
				return Notice{}, textAbsent, err
			}
		}
	}
	// A device is listed for a rerun by its requested records only: a
	// session-init failure under on-error continue does not fail the
	// device.
	if failed && r.CommandKind != "session_init" {
		s.failed[r.Device.CanonicalName] = true
	}
	text = s.texts[r.Device.CanonicalName]
	if text == textAbsent {
		s.texts[r.Device.CanonicalName] = textOpen
	}
	// A spooled record leaves with its Output empty: the follower is fed
	// from the file at the notice's offset, and the renderer streams from
	// the source.
	if s.onDurable != nil {
		s.onDurable(r, notice)
	}
	return notice, text, nil
}

// appendCommandsLine writes a line of the measured length to
// commands.jsonl under the job's byte limit.
func (s *Store) appendCommandsLine(line recordLine, length int64) error {
	if s.maxBytes > 0 && s.bytes+s.textBytes+length > s.maxBytes {
		return jobLimitExceeded(s.maxBytes)
	}
	n, err := writeLine(s.commands, line)
	if err != nil {
		return err
	}
	if n != length {
		return errorcodes.Errorf("output_commands_short_write", "short commands.jsonl write")
	}
	if s.fsync {
		if err := s.commands.Sync(); err != nil {
			return err
		}
	}
	s.bytes += n
	return nil
}

// writeLine writes a record's line through one buffer, so a line in pieces
// reaches the file in writes of the buffer's size and a whole line in one.
func writeLine(w io.Writer, line recordLine) (int64, error) {
	buffered := bufio.NewWriterSize(w, 256<<10)
	n, err := line.WriteTo(buffered)
	if err != nil {
		return n, err
	}
	return n, buffered.Flush()
}

func (s *Store) Bytes() int64 { s.mu.Lock(); defer s.mu.Unlock(); return s.bytes }
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	// A device still open at the close had no end (a cancel, a shutdown):
	// its temporary goes and its previous file stays.
	for name, cf := range s.collections {
		s.endCollection(name, cf, false)
	}
	names := make([]string, 0, len(s.failed))
	for n := range s.failed {
		names = append(names, n)
	}
	sort.Strings(names)
	content := ""
	if len(names) > 0 {
		content = strings.Join(names, "\n") + "\n"
	}
	if !s.skip.FailedDevicesTxt {
		if err := atomicBytes(s.paths.FailedDevices, []byte(content), osutil.OutputFileMode); err != nil {
			return err
		}
	}
	// Close on a nil *os.File (a skipped file) is os.ErrInvalid, not a fault.
	var first error
	for _, f := range []*os.File{s.commands, s.failures} {
		if f == nil {
			continue
		}
		if err := f.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
func EncodeOutput(raw []byte) (text, encoding, digest string) {
	sum := sha256.Sum256(raw)
	digest = hex.EncodeToString(sum[:])
	if utf8.Valid(raw) {
		return string(raw), "utf-8", digest
	}
	return base64.StdEncoding.EncodeToString(raw), "base64", digest
}
func FreeBytes(path string) (int64, error) {
	probe := path
	for {
		var st syscall.Statfs_t
		if err := syscall.Statfs(probe, &st); err == nil {
			return int64(st.Bavail) * int64(st.Bsize), nil
		} else if !os.IsNotExist(err) {
			return 0, err
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return 0, os.ErrNotExist
		}
		probe = parent
	}
}

// atomicBytes writes data to path by exclusive temporary file and rename:
// the temporary file never follows a link, and the rename
// replaces a link at path rather than writing through it.
func atomicBytes(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".karvi-atomic-")
	if err != nil {
		return err
	}
	name := f.Name()
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(name)
		}
	}()
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err == nil {
		err = d.Sync()
		d.Close()
	}
	if err != nil {
		return err
	}
	ok = true
	return nil
}

// collectionOK is the collection rule for one record: succeeded, or a statement the
// device rejected (device_error with the code device_command_error). A
// rejected paging command at set-up is device_error with another code and
// is a session error.
func collectionOK(r *records.CommandRecord) bool {
	if r.Status == "succeeded" {
		return true
	}
	return r.Status == "device_error" && r.Error != nil && r.Error.Code == "device_command_error"
}

// appendCollection is the collection's part of AppendRecord (12.3, 12.4):
// every record of the device counts towards its outcome; a requested
// record that is not a failure is written as a block under its marker,
// into a temporary opened at the device's first block. A block that
// cannot be written makes the device a failure, said once.
func (s *Store) appendCollection(r *records.CommandRecord, src Source) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := r.Device.CanonicalName
	cf := s.collections[name]
	if cf == nil {
		cf = &collectionFile{}
		s.collections[name] = cf
	}
	if cf.failed {
		return
	}
	if !collectionOK(r) {
		cf.failed = true
		return
	}
	if r.CommandKind != "requested" {
		return
	}
	if err := s.writeCollectionBlock(cf, name, r, src); err != nil {
		cf.failed = true
		if s.warn != nil {
			s.warn(fmt.Sprintf("collection_write_failed: %s is not collected from record %d on: %v; its previous file is kept", name, r.Sequence, err))
		}
	}
}

// writeCollectionBlock opens the device's temporary at its first block
// (sweeping any temporary a killed process left for the same device, 12.3
// rule 6) and writes the block: a blank line before every marker but the
// first, the marker, the output as the device sent it (12.4).
func (s *Store) writeCollectionBlock(cf *collectionFile, name string, r *records.CommandRecord, src Source) error {
	if cf.f == nil {
		file := FileName(name, s.crop)
		stale, _ := filepath.Glob(filepath.Join(s.collection.Directory, "."+file+".*"))
		for _, p := range stale {
			os.Remove(p)
		}
		cf.tmp = filepath.Join(s.collection.Directory, "."+file+"."+s.id)
		f, err := os.OpenFile(cf.tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, s.collection.FileMode)
		if err != nil {
			return err
		}
		// The mode explicitly, so the umask cannot narrow it.
		if err := f.Chmod(s.collection.FileMode); err != nil {
			f.Close()
			os.Remove(cf.tmp)
			return err
		}
		cf.f, cf.w = f, bufio.NewWriter(f)
	}
	if cf.blocks > 0 {
		if err := cf.w.WriteByte('\n'); err != nil {
			return err
		}
	}
	if _, err := cf.w.WriteString("! " + r.Command + "\n"); err != nil {
		return err
	}
	// The platform's drop list, between the answer and the file (12.19
	// rule 2): the marker above is never matched.
	w := io.Writer(cf.w)
	var lf *lineFilter
	if res := s.filters[r.Platform]; len(res) > 0 {
		lf = &lineFilter{w: cf.w, res: res}
		w = lf
	}
	if err := writeTextAnswer(w, src); err != nil {
		return err
	}
	if lf != nil {
		if err := lf.flush(); err != nil {
			return err
		}
	}
	cf.blocks++
	return nil
}

// EndDevice is the executor's word that a device's records are all
// appended, the mirror of SetTextSetup before its first: a
// device whose every record passed has its temporary synced and renamed
// into place in one step; any other device keeps its previous file and
// its temporary goes. Nothing happens for a job without a collection.
func (s *Store) EndDevice(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.collection == nil {
		return
	}
	cf := s.collections[name]
	if cf == nil {
		cf = &collectionFile{failed: true}
	}
	s.endCollection(name, cf, !cf.failed)
}

// endCollection closes a device's collection under the lock: replaced
// when ok, kept otherwise. A rename that fails is a kept file, said once.
func (s *Store) endCollection(name string, cf *collectionFile, ok bool) {
	delete(s.collections, name)
	file := FileName(name, s.crop)
	outcome := records.CollectionKept
	if cf.f != nil {
		err := cf.w.Flush()
		if err == nil && ok && s.fsync {
			err = cf.f.Sync()
		}
		if closeErr := cf.f.Close(); err == nil {
			err = closeErr
		}
		if err == nil && ok {
			err = os.Rename(cf.tmp, filepath.Join(s.collection.Directory, file))
		}
		if err == nil && ok {
			outcome = records.CollectionReplaced
		} else {
			os.Remove(cf.tmp)
			if ok && s.warn != nil {
				s.warn(fmt.Sprintf("collection_write_failed: %s is not collected: %v; its previous file is kept", name, err))
			}
		}
	}
	s.outcomes[name] = records.CollectionDevice{File: file, Outcome: outcome}
}

// CollectionSummary is the summary's collection block, nil
// for a job without a collection.
func (s *Store) CollectionSummary() *records.CollectionSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.collection == nil {
		return nil
	}
	out := &records.CollectionSummary{Directory: s.collection.Directory, Devices: map[string]records.CollectionDevice{}}
	for name, d := range s.outcomes {
		out.Devices[name] = d
		if d.Outcome == records.CollectionReplaced {
			out.Replaced++
		} else {
			out.Kept++
		}
	}
	return out
}

// CollectionLabel is the result line's tail for a collection:
// " collection=DIR replaced=N kept=M", empty for a job without one.
func CollectionLabel(c *records.CollectionSummary) string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf(" collection=%s replaced=%d kept=%d", c.Directory, c.Replaced, c.Kept)
}
