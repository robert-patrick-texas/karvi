package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// publish writes the site beside out, checks it there, and only then puts
// it in out's place, so out is always one whole build. out is replaced when
// it is missing, empty, or carries the marker; any other directory, a
// symbolic link, or a file is refused and left as it is. A failed write or
// check removes the new directory and leaves out as it was.
func publish(out string, files site) (err error) {
	out = filepath.Clean(out)
	if err := replaceable(out); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(out), filepath.Base(out)+".new-")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(tmp)
		}
	}()
	if err := write(tmp, files); err != nil {
		return err
	}
	if problems := check(tmp); len(problems) > 0 {
		return fmt.Errorf("%d link(s) do not resolve:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
	if err := replaceable(out); err != nil {
		return err
	}
	return swap(tmp, out)
}

// replaceable says whether out may be replaced.
func replaceable(out string) error {
	fi, err := os.Lstat(out)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case fi.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("%s is a symbolic link; it is not replaced: name the directory itself with -out", out)
	case !fi.IsDir():
		return fmt.Errorf("%s is not a directory; it is not replaced", out)
	}
	if _, err := os.Stat(filepath.Join(out, marker)); err == nil {
		return nil
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s holds files and was not made by md-to-html (no %s); it is not replaced: move it away or name another -out", out, marker)
	}
	return nil
}

// write writes the files under dir, directories 0755 and files 0644
// whatever the umask, so a site can serve the directory as it is.
func write(dir string, files site) error {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	if err := os.Chmod(dir, 0o755); err != nil {
		return err
	}
	for _, name := range names {
		p := filepath.Join(dir, filepath.FromSlash(name))
		for d := filepath.Dir(p); d != dir; d = filepath.Dir(d) {
			if err := os.MkdirAll(d, 0o755); err != nil {
				return err
			}
			if err := os.Chmod(d, 0o755); err != nil {
				return err
			}
		}
		if err := os.WriteFile(p, files[name], 0o644); err != nil {
			return err
		}
		if err := os.Chmod(p, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// swap puts tmp in out's place: the old directory renamed aside, the new
// one renamed in, the old one removed; an empty out is removed first.
func swap(tmp, out string) error {
	fi, err := os.Lstat(out)
	if errors.Is(err, fs.ErrNotExist) {
		return os.Rename(tmp, out)
	}
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(out, marker)); err != nil && fi.IsDir() {
		if err := os.Remove(out); err != nil { // empty, as replaceable found it
			return err
		}
		return os.Rename(tmp, out)
	}
	old, err := os.MkdirTemp(filepath.Dir(out), filepath.Base(out)+".old-")
	if err != nil {
		return err
	}
	if err := os.Remove(old); err != nil {
		return err
	}
	if err := os.Rename(out, old); err != nil {
		return err
	}
	if err := os.Rename(tmp, out); err != nil {
		if back := os.Rename(old, out); back != nil {
			return fmt.Errorf("%w; the previous site is at %s", err, old)
		}
		return err
	}
	return os.RemoveAll(old)
}
