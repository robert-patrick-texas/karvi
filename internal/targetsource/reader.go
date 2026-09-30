// Package targetsource reads target names for --tf and --tfr: one file,
// standard input, the files directly in a folder, or a folder tree. It yields
// names in order and reports each failure under its own code; the caller
// applies the
// empty-source rule to what it yields.
package targetsource

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// Options selects a plain (--tf) or recursive (--tfr) read.
type Options struct {
	// Recursive reads subfolders and links to subfolders.
	Recursive bool
	// MaxDepth is targets.recursion-max-depth: PATH is level 0, so 3 reads up
	// to three subfolder levels. Ignored unless Recursive.
	MaxDepth int
}

// Lines yields the names of one target file: one name per line
// with surrounding whitespace removed; empty lines and lines whose first
// non-whitespace character is # or ! are skipped, a "!" line being a comment
// that removes nothing. source names the input in errors.
func Lines(r io.Reader, source string) ([]string, error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	out := []string{}
	for s.Scan() {
		v := strings.TrimSpace(s.Text())
		if v != "" && !strings.HasPrefix(v, "#") && !strings.HasPrefix(v, "!") {
			out = append(out, v)
		}
	}
	if err := s.Err(); err != nil {
		return nil, coded(source, err)
	}
	return out, nil
}

// Skip reports whether a folder entry is left unread:
// names beginning with "."; names beginning with "readme" or "disabled" in
// any letter case; names ending in "~", ".bak", ".swp", ".orig", or ".rej" in
// any letter case; and names of the form "#name#". The rules apply to files
// and, in a recursive read, to folders. The path the operator names is never
// subject to them.
func Skip(name string) bool {
	lower := strings.ToLower(name)
	switch {
	case strings.HasPrefix(name, "."):
		return true
	case strings.HasPrefix(lower, "readme"), strings.HasPrefix(lower, "disabled"):
		return true
	case strings.HasSuffix(name, "~"):
		return true
	case strings.HasSuffix(lower, ".bak"), strings.HasSuffix(lower, ".swp"), strings.HasSuffix(lower, ".orig"), strings.HasSuffix(lower, ".rej"):
		return true
	case len(name) >= 2 && strings.HasPrefix(name, "#") && strings.HasSuffix(name, "#"):
		return true
	}
	return false
}

// Read yields the names a --tf or --tfr path supplies, in order. A regular
// file, or a link to one, is read as Lines. A folder
// is read in byte order of entry names: regular files and links to regular
// files are read; subfolders and links to folders are skipped, or followed
// when opts.Recursive; a pipe, socket, or device file fails with
// target_source_special_file. A folder deeper than opts.MaxDepth fails with
// target_source_depth_exceeded; a folder reached twice through links is read
// once. A missing path fails with target_source_missing, a permission error
// with target_source_permission_denied, and any other failure with
// target_source_unreadable. Empty files and folders yield nothing without
// error; the caller applies targets.empty-source.
func Read(path string, opts Options) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, coded(path, err)
	}
	switch {
	case info.Mode().IsRegular():
		return readFile(path)
	case info.IsDir():
		r := &reader{opts: opts, visited: map[string]bool{}}
		return r.dir(path, 0)
	default:
		return nil, special(path, info.Mode())
	}
}

type reader struct {
	opts    Options
	visited map[string]bool
	names   []string
}

func (r *reader) dir(dir string, depth int) ([]string, error) {
	key, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, coded(dir, err)
	}
	if r.visited[key] {
		return r.names, nil
	}
	r.visited[key] = true
	entries, err := os.ReadDir(dir) // sorted by name, byte order
	if err != nil {
		return nil, coded(dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if Skip(name) {
			continue
		}
		full := filepath.Join(dir, name)
		var info fs.FileInfo
		if e.Type()&fs.ModeSymlink != 0 {
			info, err = os.Stat(full) // follow the link
		} else {
			info, err = e.Info()
		}
		if err != nil {
			return nil, coded(full, err)
		}
		switch {
		case info.Mode().IsRegular():
			lines, err := readFile(full)
			if err != nil {
				return nil, err
			}
			r.names = append(r.names, lines...)
		case info.IsDir():
			if !r.opts.Recursive {
				continue
			}
			if depth+1 > r.opts.MaxDepth {
				return nil, errorcodes.Errorf("target_source_depth_exceeded", "target folder %s is deeper than targets.recursion-max-depth %d", full, r.opts.MaxDepth)
			}
			if _, err := r.dir(full, depth+1); err != nil {
				return nil, err
			}
		default:
			return nil, special(full, info.Mode())
		}
	}
	if r.names == nil {
		r.names = []string{}
	}
	return r.names, nil
}

func readFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, coded(path, err)
	}
	defer f.Close()
	return Lines(f, path)
}

func special(path string, mode fs.FileMode) error {
	kind := "special file"
	switch {
	case mode&fs.ModeNamedPipe != 0:
		kind = "named pipe"
	case mode&fs.ModeSocket != 0:
		kind = "socket"
	case mode&fs.ModeDevice != 0:
		kind = "device file"
	}
	return errorcodes.Errorf("target_source_special_file", "target source %s is a %s; only regular files and folders are read", path, kind)
}

// coded maps an operating-system failure to its target_source code.
func coded(path string, err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errorcodes.Errorf("target_source_missing", "target source %s does not exist: %w", path, err)
	case errors.Is(err, fs.ErrPermission):
		return errorcodes.Errorf("target_source_permission_denied", "target source %s: %w", path, err)
	}
	return errorcodes.Errorf("target_source_unreadable", "target source %s: %w", path, err)
}
