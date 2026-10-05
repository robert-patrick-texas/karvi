package termtext

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ParseTiming reads util-linux script(1)'s advanced timing log (`-T FILE -m
// advanced`) for the widths the terminal had: the starting columns from the
// `H ... COLUMNS N` header, and a Resize at each `S ... SIGWINCH ROWS=R
// COLS=C` line, its offset the output bytes counted by the `O` lines before
// it. The offsets are into the output with script(1)'s marker lines removed.
// Input lines (`I`, with --log-in) and other headers are passed over; a line
// of another shape is an error naming it.
func ParseTiming(in io.Reader) (columns int, resizes []Resize, err error) {
	sc := bufio.NewScanner(in)
	var offset int64
	for n := 1; sc.Scan(); n++ {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		if len(f) < 3 {
			return 0, nil, fmt.Errorf("timing log line %d: %q is not TYPE DELAY VALUE", n, sc.Text())
		}
		switch f[0] {
		case "O":
			k, err := strconv.ParseInt(f[2], 10, 64)
			if err != nil || k < 0 {
				return 0, nil, fmt.Errorf("timing log line %d: output count %q", n, f[2])
			}
			offset += k
		case "S":
			if f[2] != "SIGWINCH" {
				continue
			}
			c := -1
			for _, kv := range f[3:] {
				if v, ok := strings.CutPrefix(kv, "COLS="); ok {
					c, err = strconv.Atoi(v)
					if err != nil || c < 0 {
						return 0, nil, fmt.Errorf("timing log line %d: columns %q", n, v)
					}
				}
			}
			if c < 0 {
				return 0, nil, fmt.Errorf("timing log line %d: a resize without COLS", n)
			}
			resizes = append(resizes, Resize{Offset: offset, Columns: c})
		case "H":
			if f[2] == "COLUMNS" && len(f) > 3 {
				columns, err = strconv.Atoi(f[3])
				if err != nil || columns < 0 {
					return 0, nil, fmt.Errorf("timing log line %d: columns %q", n, f[3])
				}
			}
		case "I":
		default:
			return 0, nil, fmt.Errorf("timing log line %d: unknown type %q", n, f[0])
		}
	}
	if err := sc.Err(); err != nil {
		return 0, nil, err
	}
	return columns, resizes, nil
}
