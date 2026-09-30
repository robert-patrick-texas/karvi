// Command secret-scan finds seeded canaries in files, trees, standard input,
// the environment, and processes, in every encoding a leak would take. The
// release check runs it over a finished state tree.
//
//	secret-scan [-canary VALUE]... [-canary-env NAME]... [-proc PID]... [-env] [PATH...]
//
// Canaries come from flags or from named environment variables, so a script
// never puts one on a command line another process could read. Exit 0 with
// a "scanned:" line when clean, 1 on any hit (one "source:offset:encoding"
// line each), 2 on usage or an unreadable path. Symbolic links are not
// followed; "-" reads standard input.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/robert-patrick-texas/karvi/internal/canary"
)

type list []string

func (l *list) String() string     { return fmt.Sprint([]string(*l)) }
func (l *list) Set(s string) error { *l = append(*l, s); return nil }

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var values, envNames, pids list
	var scanEnv bool
	fs := flag.NewFlagSet("secret-scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Var(&values, "canary", "canary value to find (repeatable)")
	fs.Var(&envNames, "canary-env", "environment variable holding a canary value (repeatable)")
	fs.Var(&pids, "proc", "process ID whose command line and environment are scanned (repeatable)")
	fs.BoolVar(&scanEnv, "env", false, "scan this process's environment")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var seeds []canary.Value
	for _, v := range values {
		seeds = append(seeds, canary.Value{Raw: v})
	}
	for _, name := range envNames {
		v, ok := os.LookupEnv(name)
		if !ok || v == "" {
			fmt.Fprintf(stderr, "secret-scan: environment variable %s is unset or empty\n", name)
			return 2
		}
		seeds = append(seeds, canary.Value{Raw: v})
	}
	if len(seeds) == 0 {
		fmt.Fprintln(stderr, "secret-scan: at least one -canary or -canary-env is required")
		return 2
	}
	if fs.NArg() == 0 && len(pids) == 0 && !scanEnv {
		fmt.Fprintln(stderr, "secret-scan: nothing to scan; give PATH, -, -proc, or -env")
		return 2
	}
	var hits []canary.Hit
	files, bytes := 0, int64(0)
	for _, path := range fs.Args() {
		if path == "-" {
			data, err := io.ReadAll(stdin)
			if err != nil {
				fmt.Fprintf(stderr, "secret-scan: stdin: %v\n", err)
				return 2
			}
			files++
			bytes += int64(len(data))
			hits = append(hits, canary.Scan("stdin", data, seeds...)...)
			continue
		}
		res, err := canary.ScanTree(path, seeds...)
		if err != nil {
			fmt.Fprintf(stderr, "secret-scan: %s: %v\n", path, err)
			return 2
		}
		files += res.Files
		bytes += res.Bytes
		hits = append(hits, res.Hits...)
	}
	for _, p := range pids {
		pid, err := strconv.Atoi(p)
		if err != nil {
			fmt.Fprintf(stderr, "secret-scan: -proc %q is not a PID\n", p)
			return 2
		}
		ph, err := canary.ScanProcess(pid, seeds...)
		if err != nil {
			fmt.Fprintf(stderr, "secret-scan: %v\n", err)
			return 2
		}
		hits = append(hits, ph...)
	}
	if scanEnv {
		hits = append(hits, canary.ScanEnviron(os.Environ(), seeds...)...)
	}
	for _, h := range hits {
		fmt.Fprintln(stdout, h.String())
	}
	if len(hits) > 0 {
		fmt.Fprintf(stdout, "hits: %d\n", len(hits))
		return 1
	}
	fmt.Fprintf(stdout, "scanned: %d files, %d bytes, %d processes; 0 hits\n", files, bytes, len(pids))
	return 0
}
