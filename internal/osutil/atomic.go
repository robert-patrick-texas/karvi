package osutil

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// JSONIndent is the indentation of every .json file karvi writes: a file an
// operator opens is multi-line. A .jsonl file
// is one compact object per line and never uses it.
const JSONIndent = "  "

// AtomicJSON writes value to path as indented JSON ending in a newline,
// through a temporary file of mode in the same directory, renamed into place
// and synced with its directory. No reader sees a partial file.
func AtomicJSON(path string, value any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".karvi-atomic-")
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
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	enc := json.NewEncoder(tmp)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", JSONIndent)
	if err := enc.Encode(value); err != nil {
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
	dir, err := os.Open(filepath.Dir(path))
	if err == nil {
		err = dir.Sync()
		dir.Close()
	}
	if err != nil {
		return fmt.Errorf("sync parent directory: %w", err)
	}
	ok = true
	return nil
}
func AppendJSONLine(f *os.File, value any, fsync bool) (int64, int, error) {
	start, err := f.Seek(0, 2)
	if err != nil {
		return 0, 0, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return 0, 0, err
	}
	data = append(data, '\n')
	n, err := f.Write(data)
	if err != nil {
		return start, n, err
	}
	if n != len(data) {
		return start, n, errorcodes.Errorf("jsonl_short_write", "short write: %d/%d", n, len(data))
	}
	if fsync {
		if err := f.Sync(); err != nil {
			return start, n, err
		}
	}
	return start, n, nil
}
