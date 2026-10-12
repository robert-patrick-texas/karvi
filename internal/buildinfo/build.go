package buildinfo

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
)

// ownExecutable is the file the process reads its own build from: the
// executable it runs, even after its file was replaced or removed.
var ownExecutable = "/proc/self/exe"

// build reads this executable's build once per process.
var build = sync.OnceValues(func() (string, error) { return readBuild(ownExecutable) })

// Build is this executable's build: the last part of the Go build ID its
// executable carries, contentID(binary), the 20 characters Go derives from
// the executable's own bytes (EXAMPLES chapter 57, issues 1 and 2). Two
// programs of one tree that differ in a byte are two builds, which the
// version and the commit cannot tell apart. The ID is read from the ELF note
// .note.go.buildid of /proc/self/exe, some 10 µs, and never hashed; an
// executable linked with -buildid= has none, and Build returns an error.
func Build() (string, error) { return build() }

// BuildOrNone is Build for a display: "none" when it cannot be read.
func BuildOrNone() string {
	if b, err := Build(); err == nil {
		return b
	}
	return "none"
}

// readBuild returns the last part of the Go build ID in path's ELF note.
func readBuild(path string) (string, error) {
	f, err := elf.Open(path)
	if err != nil {
		return "", fmt.Errorf("read the Go build ID of %s: %w", path, err)
	}
	defer f.Close()
	section := f.Section(".note.go.buildid")
	if section == nil {
		return "", fmt.Errorf("%s carries no Go build ID (.note.go.buildid)", path)
	}
	data, err := section.Data()
	if err != nil {
		return "", fmt.Errorf("read the Go build ID of %s: %w", path, err)
	}
	// One note: namesz, descsz, and type, then the name ("Go") padded to
	// four bytes, then the build ID.
	if len(data) < 12 {
		return "", fmt.Errorf("%s: the Go build ID note is short", path)
	}
	nameSize := binary.LittleEndian.Uint32(data[0:4])
	descSize := binary.LittleEndian.Uint32(data[4:8])
	start := 12 + uint64(nameSize+3)&^3
	if uint64(len(data)) < start+uint64(descSize) {
		return "", fmt.Errorf("%s: the Go build ID note is short", path)
	}
	id := string(data[start : start+uint64(descSize)])
	part := id[strings.LastIndexByte(id, '/')+1:]
	if part == "" {
		return "", fmt.Errorf("%s: the Go build ID %q names no build", path, id)
	}
	return part, nil
}
