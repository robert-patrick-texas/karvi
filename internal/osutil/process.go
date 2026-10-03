package osutil

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

type CPUProfile struct {
	AffinityCPUs       int    `json:"affinity_cpus"`
	CgroupQuotaCPUs    int    `json:"cgroup_quota_cpus"`
	EffectiveCPUs      int    `json:"effective_cpu_count"`
	GOMAXPROCS         int    `json:"gomaxprocs"`
	PreviousGOMAXPROCS int    `json:"previous_gomaxprocs,omitempty"`
	Source             string `json:"source"`
}

func EffectiveCPU() CPUProfile {
	aff := affinityCount()
	quota := cgroupQuota()
	effective := runtime.NumCPU()
	source := "runtime.NumCPU"
	if aff > 0 && aff < effective {
		effective = aff
		source = "affinity"
	}
	if quota > 0 && quota < effective {
		effective = quota
		source = "cgroup"
	}
	if effective < 1 {
		effective = 1
	}
	return CPUProfile{AffinityCPUs: aff, CgroupQuotaCPUs: quota, EffectiveCPUs: effective, GOMAXPROCS: runtime.GOMAXPROCS(0), Source: source}
}
func ApplyGOMAXPROCS() CPUProfile {
	p := EffectiveCPU()
	p.PreviousGOMAXPROCS = runtime.GOMAXPROCS(0)
	runtime.GOMAXPROCS(p.EffectiveCPUs)
	p.GOMAXPROCS = runtime.GOMAXPROCS(0)
	return p
}
func affinityCount() int {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if strings.HasPrefix(s.Text(), "Cpus_allowed_list:") {
			return parseCPUList(strings.TrimSpace(strings.TrimPrefix(s.Text(), "Cpus_allowed_list:")))
		}
	}
	return 0
}
func parseCPUList(s string) int {
	total := 0
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			p := strings.SplitN(part, "-", 2)
			a, e1 := strconv.Atoi(p[0])
			b, e2 := strconv.Atoi(p[1])
			if e1 == nil && e2 == nil && b >= a {
				total += b - a + 1
			}
		} else if _, err := strconv.Atoi(part); err == nil {
			total++
		}
	}
	return total
}
func cgroupQuota() int {
	data, err := os.ReadFile("/sys/fs/cgroup/cpu.max")
	if err == nil {
		f := strings.Fields(string(data))
		if len(f) >= 2 && f[0] != "max" {
			q, e1 := strconv.ParseInt(f[0], 10, 64)
			p, e2 := strconv.ParseInt(f[1], 10, 64)
			if e1 == nil && e2 == nil && q > 0 && p > 0 {
				return int(q / p)
			}
		}
	}
	q, eq := readInt("/sys/fs/cgroup/cpu/cpu.cfs_quota_us")
	p, ep := readInt("/sys/fs/cgroup/cpu/cpu.cfs_period_us")
	if eq == nil && ep == nil && q > 0 && p > 0 {
		return int(q / p)
	}
	return 0
}
func readInt(path string) (int64, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return 0, e
	}
	return strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
}

type Limits struct {
	NoFileSoft uint64   `json:"nofile_soft"`
	NoFileHard uint64   `json:"nofile_hard"`
	NProcSoft  uint64   `json:"nproc_soft"`
	NProcHard  uint64   `json:"nproc_hard"`
	Warnings   []string `json:"warnings"`
}

func RaiseLimits() Limits {
	out := Limits{}
	var r syscall.Rlimit
	if syscall.Getrlimit(syscall.RLIMIT_NOFILE, &r) == nil {
		if r.Cur < r.Max {
			r.Cur = r.Max
			_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &r)
			_ = syscall.Getrlimit(syscall.RLIMIT_NOFILE, &r)
		}
		out.NoFileSoft, out.NoFileHard = r.Cur, r.Max
		if r.Cur <= 1024 {
			out.Warnings = append(out.Warnings, fmt.Sprintf("RLIMIT_NOFILE remains constrained at %d", r.Cur))
		}
	}
	const rlimitNProc = 6
	if syscall.Getrlimit(rlimitNProc, &r) == nil {
		if r.Cur < r.Max {
			r.Cur = r.Max
			_ = syscall.Setrlimit(rlimitNProc, &r)
			_ = syscall.Getrlimit(rlimitNProc, &r)
		}
		out.NProcSoft, out.NProcHard = r.Cur, r.Max
		if r.Cur <= 1024 {
			out.Warnings = append(out.Warnings, fmt.Sprintf("RLIMIT_NPROC remains constrained at %d", r.Cur))
		}
	}
	return out
}
func BootID() string {
	b, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return strings.TrimSpace(string(b))
}
func ProcessStartIdentity(pid int) string {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ""
	}
	s := string(b)
	idx := strings.LastIndex(s, ")")
	if idx < 0 {
		return ""
	}
	fields := strings.Fields(s[idx+1:])
	if len(fields) > 19 {
		return fields[19]
	}
	return ""
}

// ProcessAlive reports whether pid is a live process and, when start is
// given, the one that started then (ProcessStartIdentity). A process of
// another user is alive: the signal check answers EPERM for it, and the
// shared capacity ledger holds every operator's leases.
func ProcessAlive(pid int, start string) bool {
	if pid <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil && err != syscall.EPERM {
		return false
	}
	return start == "" || ProcessStartIdentity(pid) == start
}

// ProcessCommandName is the base name of a live process's first argument
// (/proc/PID/cmdline), empty when the process is gone or unreadable: the
// spool sweep's owner check, which keeps a spool
// whose pid is alive as a karvi executable and removes one whose pid died
// or was reused by something else.
func ProcessCommandName(pid int) string {
	if pid <= 0 {
		return ""
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return ""
	}
	first, _, _ := strings.Cut(string(b), "\x00")
	return filepath.Base(first)
}
