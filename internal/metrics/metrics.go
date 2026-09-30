// Package metrics provides local, per-job CPU/process sampling and persisted
// bottleneck evidence. It exposes no network listener.
package metrics

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/records"
)

type cpuCounters struct{ idle, total uint64 }
type Sampler struct {
	mu                 sync.Mutex
	jobID              string
	start              time.Time
	interval, halfLife time.Duration
	warmup             int
	threshold, zone    float64
	signal             float64
	valid              int
	cpu                []map[string]any
	process            []map[string]any
	waves              []map[string]any
	stages             map[string][]int64
	cancel             context.CancelFunc
	done               chan struct{}
	profile            osutil.CPUProfile
	limits             osutil.Limits
}

func New(jobID string, interval, halfLife time.Duration, warmup int, threshold, zone float64) *Sampler {
	if interval <= 0 {
		interval = time.Second
	}
	if halfLife <= 0 {
		halfLife = 15 * time.Second
	}
	if warmup < 1 {
		warmup = 5
	}
	return &Sampler{jobID: jobID, start: time.Now(), interval: interval, halfLife: halfLife, warmup: warmup, threshold: threshold, zone: zone, stages: map[string][]int64{}, done: make(chan struct{}), profile: osutil.EffectiveCPU(), limits: osutil.RaiseLimits()}
}
func (s *Sampler) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	go s.loop(ctx)
}
func (s *Sampler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
	}
}
func (s *Sampler) Current() float64 { s.mu.Lock(); defer s.mu.Unlock(); return s.signal }
func (s *Sampler) RecordWave(wave int, old, new int, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.waves = append(s.waves, map[string]any{"wave": wave, "signal": s.signal, "old_width": old, "new_width": new, "reason": reason})
}
func (s *Sampler) AddStage(name string, d time.Duration) {
	s.mu.Lock()
	s.stages[name] = append(s.stages[name], d.Nanoseconds())
	s.mu.Unlock()
}
func (s *Sampler) loop(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	prev, ok := readCPU()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			next, valid := readCPU()
			instant := 0.0
			if ok && valid {
				idle := next.idle - prev.idle
				total := next.total - prev.total
				if total > 0 && next.idle >= prev.idle && next.total >= prev.total {
					instant = 100 * float64(total-idle) / float64(total)
					s.update(now, instant, true)
				} else {
					s.update(now, 0, false)
				}
			} else {
				s.update(now, 0, false)
			}
			prev, ok = next, valid
		}
	}
}
func (s *Sampler) update(now time.Time, instant float64, valid bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if valid {
		s.valid++
		if s.valid <= s.warmup {
			s.signal = (s.signal*float64(s.valid-1) + instant) / float64(s.valid)
		} else {
			alpha := 1 - math.Exp(-math.Ln2*s.interval.Seconds()/s.halfLife.Seconds())
			s.signal = alpha*instant + (1-alpha)*s.signal
		}
	}
	s.cpu = append(s.cpu, map[string]any{"offset_ns": now.Sub(s.start).Nanoseconds(), "instant_percent": instant, "ewma_percent": s.signal, "valid": valid})
	s.process = append(s.process, processSample(now.Sub(s.start)))
}
func readCPU() (cpuCounters, bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuCounters{}, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return cpuCounters{}, false
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 9 || fields[0] != "cpu" {
		return cpuCounters{}, false
	}
	nums := make([]uint64, 8)
	for i := 0; i < 8; i++ {
		n, err := strconv.ParseUint(fields[i+1], 10, 64)
		if err != nil {
			return cpuCounters{}, false
		}
		nums[i] = n
	}
	return cpuCounters{idle: nums[3] + nums[4], total: nums[0] + nums[1] + nums[2] + nums[3] + nums[4] + nums[5] + nums[6] + nums[7]}, true
}
func processSample(offset time.Duration) map[string]any {
	rss := int64(0)
	if b, err := os.ReadFile("/proc/self/statm"); err == nil {
		f := strings.Fields(string(b))
		if len(f) > 1 {
			pages, _ := strconv.ParseInt(f[1], 10, 64)
			rss = pages * int64(os.Getpagesize())
		}
	}
	fds := 0
	if e, err := os.ReadDir("/proc/self/fd"); err == nil {
		fds = len(e)
	}
	return map[string]any{"offset_ns": offset.Nanoseconds(), "rss_bytes": rss, "goroutines": runtime.NumGoroutine(), "fds": fds, "child_processes": 0}
}
func (s *Sampler) Final(jobProfile map[string]any, errors map[string]any, storage map[string]any, limitations []string) records.Metrics {
	s.Stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	h := map[string]any{}
	for name, values := range s.stages {
		h[name] = histogram(values)
	}
	bottleneck := s.classify()
	return records.Metrics{SchemaVersion: 1, JobID: s.jobID, MeasurementStart: s.start, MeasurementEnd: time.Now(), HostProfile: map[string]any{"cpu": s.profile, "limits": s.limits, "kernel": kernel()}, JobProfile: jobProfile, CPUSamples: append([]map[string]any(nil), s.cpu...), ProcessSamples: append([]map[string]any(nil), s.process...), WaveDecisions: append([]map[string]any(nil), s.waves...), Admission: map[string]any{}, StageHistograms: h, Errors: errors, Storage: storage, Bottleneck: bottleneck, Limitations: limitations}
}
func (s *Sampler) classify() map[string]any {
	valid, high := 0, 0
	upper := s.threshold + s.zone
	for _, v := range s.cpu {
		if ok, _ := v["valid"].(bool); ok {
			valid++
			if ew, _ := v["ewma_percent"].(float64); ew > upper {
				high++
			}
		}
	}
	if valid < 30 {
		return map[string]any{"primary": "insufficient_data", "confidence": "low", "supporting_facts": []string{fmt.Sprintf("%d valid CPU samples", valid)}}
	}
	frac := float64(high) / float64(valid)
	if frac >= 0.5 {
		return map[string]any{"primary": "cpu", "confidence": "high", "supporting_facts": []string{fmt.Sprintf("CPU EWMA above target zone for %.1f%% of samples", frac*100)}}
	}
	return map[string]any{"primary": "device_or_network_wait", "confidence": "medium", "supporting_facts": []string{fmt.Sprintf("CPU EWMA above target zone for only %.1f%% of samples", frac*100)}}
}
func histogram(v []int64) map[string]any {
	if len(v) == 0 {
		return map[string]any{"count": 0}
	}
	x := append([]int64(nil), v...)
	sort.Slice(x, func(i, j int) bool { return x[i] < x[j] })
	sum := int64(0)
	for _, n := range x {
		sum += n
	}
	p := func(q float64) int64 {
		i := int(math.Ceil(q*float64(len(x)))) - 1
		if i < 0 {
			i = 0
		}
		if i >= len(x) {
			i = len(x) - 1
		}
		return x[i]
	}
	return map[string]any{"count": len(x), "min": x[0], "p50": p(.50), "p90": p(.90), "p95": p(.95), "p99": p(.99), "max": x[len(x)-1], "sum": sum}
}
func kernel() string {
	b, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	return strings.TrimSpace(string(b))
}
