package credhelper

import (
	"os"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// member is one process of a helper's tree, held by a pidfd: a signal sent
// through it never reaches a process that took the pid after this one
// ended. start is its start time, read before and after the pidfd was
// opened, so the pidfd is known to be this process's.
type member struct {
	pid   int
	start string
	fd    int
}

// procStat is what one /proc/PID/stat says of a process: its state, its
// parent, and its start time (field 22, in clock ticks since boot).
type procStat struct {
	state byte
	ppid  int
	start string
}

func readStat(pid int) (procStat, bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return procStat{}, false
	}
	// The command name, field 2, is in parentheses and may hold anything;
	// the fields after the last ) begin at field 3, the state.
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return procStat{}, false
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 20 || len(f[0]) == 0 {
		return procStat{}, false
	}
	ppid, err := strconv.Atoi(f[1])
	if err != nil {
		return procStat{}, false
	}
	return procStat{state: f[0][0], ppid: ppid, start: f[19]}, true
}

// descendants are the processes descended from root now, by their parents
// in /proc, each held by a pidfd (EXAMPLES chapter 56, issue 18). A process
// that has left the tree, its parent ended before it (a daemon's double
// fork), is no descendant and is left alone; so is one that ends while it is
// listed.
func descendants(root int) []member {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	children := map[int][]int{}
	stats := map[int]procStat{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if st, ok := readStat(pid); ok {
			children[st.ppid] = append(children[st.ppid], pid)
			stats[pid] = st
		}
	}
	var out []member
	queue := append([]int(nil), children[root]...)
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		queue = append(queue, children[pid]...)
		fd, err := unix.PidfdOpen(pid, 0)
		if err != nil {
			continue
		}
		if st, ok := readStat(pid); !ok || st.start != stats[pid].start {
			syscall.Close(fd)
			continue
		}
		out = append(out, member{pid: pid, start: stats[pid].start, fd: fd})
	}
	return out
}

// signalAll sends sig to every member still running.
func signalAll(tree []member, sig syscall.Signal) {
	for _, m := range tree {
		_ = unix.PidfdSendSignal(m.fd, sig, nil, 0)
	}
}

// running reports whether any member has not ended: a member is ended
// when its pid is gone, taken by another process, or a zombie waiting for
// its parent.
func running(tree []member) bool {
	for _, m := range tree {
		st, ok := readStat(m.pid)
		if ok && st.start == m.start && st.state != 'Z' && st.state != 'X' {
			return true
		}
	}
	return false
}

// release closes the members' pidfds.
func release(tree []member) {
	for _, m := range tree {
		syscall.Close(m.fd)
	}
}

// merge adds the members of more not already in tree, closing the
// duplicates' pidfds.
func merge(tree, more []member) []member {
	have := map[int]bool{}
	for _, m := range tree {
		have[m.pid] = true
	}
	for _, m := range more {
		if have[m.pid] {
			syscall.Close(m.fd)
			continue
		}
		tree = append(tree, m)
	}
	return tree
}
