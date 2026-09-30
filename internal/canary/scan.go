package canary

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// A process caught mid-exec is given this long to settle before its empty
// /proc views are believed.
const (
	execSettleAttempts = 100
	execSettleInterval = 10 * time.Millisecond
)

// TreeResult is what ScanTree saw besides its hits, so a run that scanned
// nothing cannot pass silently.
type TreeResult struct {
	Files int
	Bytes int64
	Hits  []Hit
}

// ScanTree scans every regular file below root, following no symbolic
// link. Unreadable files are errors: a scan that skips a file is not proof.
func ScanTree(root string, values ...Value) (TreeResult, error) {
	var res TreeResult
	info, err := os.Lstat(root)
	if err != nil {
		return res, err
	}
	if !info.IsDir() {
		return res, scanFile(root, &res, values...)
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		return scanFile(path, &res, values...)
	})
	return res, err
}

func scanFile(path string, res *TreeResult, values ...Value) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	res.Files++
	res.Bytes += int64(len(data))
	res.Hits = append(res.Hits, Scan(path, data, values...)...)
	return nil
}

// ScanEnviron scans NAME=value strings as os.Environ returns them. A hit
// names the variable, never its value.
func ScanEnviron(environ []string, values ...Value) []Hit {
	var hits []Hit
	for _, kv := range environ {
		name, value, _ := strings.Cut(kv, "=")
		for _, h := range Scan("env:"+name, []byte(value), values...) {
			hits = append(hits, h)
		}
	}
	return hits
}

// ScanProcess scans a process's command line and environment through
// /proc; the caller must be allowed to read them (same UID or root). A
// process still inside exec shows an empty command line and environment
// for a moment (the new memory map is installed before the argument and
// environment layout is finalized), so an empty pair is retried briefly
// before it is taken as the process's real state.
func ScanProcess(pid int, values ...Value) ([]Hit, error) {
	var hits []Hit
	base := filepath.Join("/proc", strconv.Itoa(pid))
	var cmdline, environ []byte
	for attempt := 0; ; attempt++ {
		var err error
		cmdline, err = os.ReadFile(filepath.Join(base, "cmdline"))
		if err != nil {
			return nil, fmt.Errorf("process %d: %w", pid, err)
		}
		environ, err = os.ReadFile(filepath.Join(base, "environ"))
		if err != nil {
			return nil, fmt.Errorf("process %d: %w", pid, err)
		}
		if len(cmdline) > 0 || len(environ) > 0 || attempt >= execSettleAttempts {
			break
		}
		time.Sleep(execSettleInterval)
	}
	for _, arg := range strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00") {
		hits = append(hits, Scan("proc:"+strconv.Itoa(pid)+"/cmdline", []byte(arg), values...)...)
	}
	for _, kv := range strings.Split(strings.TrimRight(string(environ), "\x00"), "\x00") {
		name, value, _ := strings.Cut(kv, "=")
		hits = append(hits, Scan("proc:"+strconv.Itoa(pid)+"/environ:"+name, []byte(value), values...)...)
	}
	return hits, nil
}
