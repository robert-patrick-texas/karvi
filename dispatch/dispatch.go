// Package dispatch provides reusable serial, fixed-parallel, and adaptive-wave
// scheduling. It schedules opaque device tasks and never handles credentials.
package dispatch

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

type Task struct {
	Key      string
	Position int
	Value    any
}
type Context struct {
	Mode                                           string
	WaveNumber, WaveWidth, WaveDepth               int
	WorkerID                                       string
	ScopePosition, DesiredWidth, EffectiveInflight int
}
type Result struct {
	Task               Task
	Success            bool
	ErrorCode          string
	External           bool
	StartedAt, EndedAt time.Time
}
type Executor interface {
	Execute(context.Context, Task, Context) Result
}
type Event struct {
	Kind   string
	Task   *Task
	Result *Result
	Wave   int
	Width  int
	// PreviousWidth is a wave decision's width before it: the wave just
	// ended ran at it, the next runs at Width.
	PreviousWidth int
	Depth         int
	Reason        string
	Counts        Counts
}
type EventSink interface{ OnEvent(Event) }
type EventSinkFunc func(Event)

func (f EventSinkFunc) OnEvent(e Event) { f(e) }

type Signal interface{ Current() float64 }
type SignalFunc func() float64

func (f SignalFunc) Current() float64 { return f() }

type Counts struct{ Total, Terminal, Succeeded, Failed, NotStarted, Inflight int }
type Plan struct {
	Mode                 string
	Tasks                []Task
	Width                int
	AbsoluteMaxWidth     int
	WaveStartWidth       int
	WaveMaxWidth         int
	WaveDepthMultiplier  int
	CPUThreshold         float64
	CPUTargetZone        float64
	StepUpPercent        float64
	StepDownPercent      float64
	CooldownWaves        int
	HaltErrorCount       int
	HaltErrorPercent     int
	HaltErrorCodes       []string
	WaveGateErrorCount   int
	WaveGateErrorPercent int
	WaveDelay            time.Duration
}
type Summary struct {
	Counts     Counts
	Results    []Result
	HaltReason string
	GateReason string
	FinalWidth int
	Waves      int
}
type RampStrategy interface {
	NextWidth(current int, ewmaCPU float64, ceiling int, floor int) int
}
type BoundedRamp struct{ Threshold, Zone, UpPercent, DownPercent float64 }

func (r BoundedRamp) NextWidth(current int, signal float64, ceiling int, floor int) int {
	lower, upper := r.Threshold-r.Zone, r.Threshold+r.Zone
	if signal < lower {
		d := max(1, int(math.Ceil(float64(current)*r.UpPercent/100)))
		return min(ceiling, current+d)
	}
	if signal > upper {
		d := max(1, int(math.Ceil(float64(current)*r.DownPercent/100)))
		return max(floor, current-d)
	}
	return current
}

type Dispatcher interface {
	Execute(context.Context, Plan, Executor, EventSink) Summary
}
type LocalDispatcher struct{ Signal Signal }

func (d LocalDispatcher) Execute(ctx context.Context, p Plan, x Executor, sink EventSink) Summary {
	normalize(&p)
	if sink == nil {
		sink = EventSinkFunc(func(Event) {})
	}
	if d.Signal == nil {
		d.Signal = SignalFunc(func() float64 { return 0 })
	}
	switch p.Mode {
	case "serial":
		p.Width = 1
		return d.executeFunnel(ctx, p, x, sink)
	case "parallel":
		return d.executeFunnel(ctx, p, x, sink)
	case "wave":
		return d.executeWaves(ctx, p, x, sink)
	default:
		return Summary{Counts: Counts{Total: len(p.Tasks), NotStarted: len(p.Tasks)}, HaltReason: "invalid_dispatch_mode"}
	}
}

// Check reports whether Execute would run the plan or refuse it at once:
// the mode must be serial, parallel, or wave.
func (p Plan) Check() error {
	switch p.Mode {
	case "", "serial", "parallel", "wave":
		return nil
	}
	return fmt.Errorf("halt_invalid_dispatch_mode: dispatch mode %q is not serial, parallel, or wave", p.Mode)
}

// Effective is the plan after the width and wave normalization Execute
// applies, for reporting.
func (p Plan) Effective() Plan {
	normalize(&p)
	return p
}

// StartWidth is the number of workers the plan starts with, after that
// normalization: one for serial, the pool for parallel, the first wave's
// width for wave.
func (p Plan) StartWidth() int {
	normalize(&p)
	if p.Mode == "wave" {
		return p.WaveStartWidth
	}
	return p.Width
}

// Describe is the plan's dispatch in one line of key=value words, as a dry
// run and an exercise report it: serial and parallel by their width, wave
// by its start, its ceiling, and its depth multiplier, since a wave job
// never runs at the parallel width.
func (p Plan) Describe() string {
	normalize(&p)
	if p.Mode == "wave" {
		return fmt.Sprintf("wave start-width=%d max-width=%d depth-multiplier=%d", p.WaveStartWidth, p.WaveMaxWidth, p.WaveDepthMultiplier)
	}
	return fmt.Sprintf("%s width=%d", p.Mode, p.Width)
}

func normalize(p *Plan) {
	if p.Mode == "" {
		p.Mode = "serial"
	}
	if p.Width < 1 || p.Mode == "serial" {
		p.Width = 1
	}
	if p.AbsoluteMaxWidth < 1 {
		p.AbsoluteMaxWidth = 512
	}
	p.Width = min(p.Width, p.AbsoluteMaxWidth)
	if p.WaveStartWidth < 1 {
		p.WaveStartWidth = p.Width
	}
	if p.WaveMaxWidth < 1 {
		p.WaveMaxWidth = p.AbsoluteMaxWidth
	}
	p.WaveMaxWidth = min(p.WaveMaxWidth, p.AbsoluteMaxWidth)
	if p.WaveStartWidth > p.WaveMaxWidth {
		p.WaveStartWidth = p.WaveMaxWidth
	}
	if p.WaveDepthMultiplier < 1 {
		p.WaveDepthMultiplier = 4
	}
	if p.CPUThreshold == 0 {
		p.CPUThreshold = 75
	}
	if p.CPUTargetZone == 0 {
		p.CPUTargetZone = 10
	}
	if p.StepUpPercent == 0 {
		p.StepUpPercent = 50
	}
	if p.StepDownPercent == 0 {
		p.StepDownPercent = 10
	}
	if p.CooldownWaves < 0 {
		p.CooldownWaves = 0
	}
}

type state struct {
	mu      sync.Mutex
	summary Summary
	stop    atomic.Bool
}

func (s *state) terminal(r Result, p Plan) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.summary.Results = append(s.summary.Results, r)
	s.summary.Counts.Terminal++
	s.summary.Counts.Inflight--
	if r.Success {
		s.summary.Counts.Succeeded++
	} else {
		s.summary.Counts.Failed++
	}
	if s.summary.HaltReason == "" {
		if !r.Success && containsString(p.HaltErrorCodes, r.ErrorCode) {
			s.summary.HaltReason = haltReasonForErrorCode(r.ErrorCode)
			s.stop.Store(true)
			return true
		}
		count := p.HaltErrorCount > 0 && s.summary.Counts.Failed >= p.HaltErrorCount
		percent := p.HaltErrorPercent > 0 && s.summary.Counts.Terminal > 0 && s.summary.Counts.Failed*100 >= p.HaltErrorPercent*s.summary.Counts.Terminal
		if count || percent {
			if count {
				s.summary.HaltReason = "error_count"
			} else {
				s.summary.HaltReason = "error_percent"
			}
			s.stop.Store(true)
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func haltReasonForErrorCode(code string) string {
	if code == "host_key_changed" || code == "host_key_changed_during_enrollment" {
		return "host_key_mismatch"
	}
	return "error_code:" + code
}
func (s *state) begin() {
	s.mu.Lock()
	s.summary.Counts.Inflight++
	s.summary.Counts.NotStarted--
	s.mu.Unlock()
}
func (s *state) snapshot() Counts { s.mu.Lock(); defer s.mu.Unlock(); return s.summary.Counts }

func (d LocalDispatcher) executeFunnel(ctx context.Context, p Plan, x Executor, sink EventSink) Summary {
	st := &state{summary: Summary{Counts: Counts{Total: len(p.Tasks), NotStarted: len(p.Tasks)}, FinalWidth: p.Width}}
	jobs := make(chan Task)
	var wg sync.WaitGroup
	for i := 0; i < p.Width; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for task := range jobs {
				if ctx.Err() != nil || st.stop.Load() {
					continue
				}
				st.begin()
				c := Context{Mode: p.Mode, WaveWidth: p.Width, WorkerID: fmt.Sprintf("w%d", worker+1), ScopePosition: task.Position, DesiredWidth: p.Width, EffectiveInflight: st.snapshot().Inflight}
				sink.OnEvent(Event{Kind: "task_started", Task: &task, Width: p.Width, Counts: st.snapshot()})
				r := x.Execute(ctx, task, c)
				st.terminal(r, p)
				sink.OnEvent(Event{Kind: "task_terminal", Task: &task, Result: &r, Width: p.Width, Counts: st.snapshot()})
			}
		}(i)
	}
loop:
	for _, task := range p.Tasks {
		select {
		case <-ctx.Done():
			st.stop.Store(true)
			break loop
		default:
		}
		if st.stop.Load() {
			break
		}
		jobs <- task
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil && st.summary.HaltReason == "" {
		st.summary.HaltReason = "cancelled"
	}
	sink.OnEvent(Event{Kind: "complete", Width: p.Width, Counts: st.summary.Counts, Reason: st.summary.HaltReason})
	return st.summary
}

func (d LocalDispatcher) executeWaves(ctx context.Context, p Plan, x Executor, sink EventSink) Summary {
	width, floor, ceiling := p.WaveStartWidth, p.WaveStartWidth, p.WaveMaxWidth
	cooldown := 0
	offset := 0
	st := &state{summary: Summary{Counts: Counts{Total: len(p.Tasks), NotStarted: len(p.Tasks)}, FinalWidth: width}}
	ramp := BoundedRamp{Threshold: p.CPUThreshold, Zone: p.CPUTargetZone, UpPercent: p.StepUpPercent, DownPercent: p.StepDownPercent}
	for offset < len(p.Tasks) && !st.stop.Load() && ctx.Err() == nil {
		wave := st.summary.Waves + 1
		depth := min(width*p.WaveDepthMultiplier, len(p.Tasks)-offset)
		cohort := p.Tasks[offset : offset+depth]
		st.summary.Waves = wave
		sink.OnEvent(Event{Kind: "wave_started", Wave: wave, Width: width, Depth: depth, Counts: st.snapshot()})
		waveFailed := 0
		waveTerminal := 0
		var wmu sync.Mutex
		jobs := make(chan Task)
		var wg sync.WaitGroup
		for i := 0; i < width; i++ {
			wg.Add(1)
			go func(worker int) {
				defer wg.Done()
				for task := range jobs {
					if ctx.Err() != nil || st.stop.Load() {
						continue
					}
					st.begin()
					c := Context{Mode: "wave", WaveNumber: wave, WaveWidth: width, WaveDepth: depth, WorkerID: fmt.Sprintf("w%d", worker+1), ScopePosition: task.Position, DesiredWidth: width, EffectiveInflight: st.snapshot().Inflight}
					sink.OnEvent(Event{Kind: "task_started", Task: &task, Wave: wave, Width: width, Depth: depth, Counts: st.snapshot()})
					r := x.Execute(ctx, task, c)
					wmu.Lock()
					waveTerminal++
					if !r.Success {
						waveFailed++
					}
					wmu.Unlock()
					st.terminal(r, p)
					sink.OnEvent(Event{Kind: "task_terminal", Task: &task, Result: &r, Wave: wave, Width: width, Depth: depth, Counts: st.snapshot()})
				}
			}(i)
		}
	waveLoop:
		for _, task := range cohort {
			if st.stop.Load() || ctx.Err() != nil {
				break waveLoop
			}
			jobs <- task
		}
		close(jobs)
		wg.Wait()
		started := waveTerminal
		offset += started
		if st.stop.Load() {
			break
		}
		countHit := p.WaveGateErrorCount > 0 && waveFailed >= p.WaveGateErrorCount
		percentHit := p.WaveGateErrorPercent > 0 && waveTerminal > 0 && waveFailed*100 >= p.WaveGateErrorPercent*waveTerminal
		if countHit || percentHit {
			if countHit {
				st.summary.GateReason = "error_count"
			} else {
				st.summary.GateReason = "error_percent"
			}
			st.stop.Store(true)
			sink.OnEvent(Event{Kind: "wave_gate", Wave: wave, Width: width, Depth: depth, Reason: st.summary.GateReason, Counts: st.snapshot()})
			break
		}
		if p.WaveDelay > 0 {
			select {
			case <-ctx.Done():
				st.stop.Store(true)
			case <-time.After(p.WaveDelay):
			}
		}
		signal := d.Signal.Current()
		old := width
		var reason string
		if cooldown > 0 {
			cooldown--
			reason = "cooldown"
		} else {
			width = ramp.NextWidth(width, signal, ceiling, floor)
			reason = decisionReason(signal, ramp, old, width)
			if width < old {
				cooldown = p.CooldownWaves
			}
		}
		st.summary.FinalWidth = width
		sink.OnEvent(Event{Kind: "wave_decision", Wave: wave, Width: width, PreviousWidth: old, Depth: min(width*p.WaveDepthMultiplier, len(p.Tasks)-offset), Reason: reason, Counts: st.snapshot()})
	}
	if ctx.Err() != nil && st.summary.HaltReason == "" {
		st.summary.HaltReason = "cancelled"
	}
	sink.OnEvent(Event{Kind: "complete", Width: st.summary.FinalWidth, Counts: st.summary.Counts, Reason: firstNonEmpty(st.summary.HaltReason, st.summary.GateReason)})
	return st.summary
}

// decisionReason names a wave decision by where the CPU signal stood
// against the ramp's band and what the width did: a signal outside the
// band that left the width where it was found it at the ceiling (below the
// band) or at the floor (above it), not in the band.
func decisionReason(signal float64, r BoundedRamp, old, width int) string {
	switch {
	case signal < r.Threshold-r.Zone && width > old:
		return "cpu_below_zone"
	case signal < r.Threshold-r.Zone:
		return "cpu_below_zone_at_ceiling"
	case signal > r.Threshold+r.Zone && width < old:
		return "cpu_above_zone"
	case signal > r.Threshold+r.Zone:
		return "cpu_above_zone_at_floor"
	}
	return "cpu_in_zone"
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
