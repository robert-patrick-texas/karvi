package output

import (
	"sort"
	"sync"
	"sync/atomic"
)

// InFlight is the running byte count of every device's command in flight
// as one atomic per device, which the device session
// stores into as bytes settle and resets when the command ends, and which
// the scoreboard's snapshot reads at its interval. Nothing is sent per
// chunk, no chunk is slowed, and a device without a counter counts 0.
type InFlight struct {
	mu       sync.Mutex
	counters map[string]*atomic.Int64
}

// Counter is the device's counter, made on first use.
func (f *InFlight) Counter(device string) *atomic.Int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.counters == nil {
		f.counters = map[string]*atomic.Int64{}
	}
	c := f.counters[device]
	if c == nil {
		c = &atomic.Int64{}
		f.counters[device] = c
	}
	return c
}

// Bytes is the device's count now, 0 without a counter.
func (f *InFlight) Bytes(device string) int64 {
	if f == nil {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if c := f.counters[device]; c != nil {
		return c.Load()
	}
	return 0
}

// Total is the sum over every device, the metrics' in_flight_bytes.
func (f *InFlight) Total() int64 {
	if f == nil {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var total int64
	for _, c := range f.counters {
		total += c.Load()
	}
	return total
}

// Devices lists the devices with a counter, sorted, for a test.
func (f *InFlight) Devices() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.counters))
	for n := range f.counters {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
