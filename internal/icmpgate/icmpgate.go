// Package icmpgate is the two-probe ICMP reachability gate. It offers one
// interface, Pinger,
// and two implementations: a Linux ping-socket pinger that sends the probes
// itself, and an adapter over the system ping executable for hosts whose
// net.ipv4.ping_group_range excludes the operator. Detect chooses between
// them once per job. Nothing here reads configuration or touches a device
// transport; the gate's policy (two probes, proceed on one reply) lives with
// its callers.
package icmpgate

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Probe count and outcome statuses.
const (
	Probes = 2

	StatusReply   = "reply"
	StatusTimeout = "timeout"
	StatusError   = "error"

	MethodSocket      = "socket"
	MethodSystem      = "system"
	MethodUnavailable = "unavailable"
)

// Outcome is one probe's result: reply with its round-trip
// time, timeout, or error with the answering address and detail.
type Outcome struct {
	Sequence int       `json:"sequence"`
	SentAt   time.Time `json:"sent_at"`
	Status   string    `json:"status"`
	RTTNS    *int64    `json:"rtt_ns"`
	From     string    `json:"from,omitempty"`
	Detail   string    `json:"detail,omitempty"`
}

// Pinger sends count echo requests to address, each with an independent
// timeout, and reports one Outcome per sequence. An error return
// means the pinger could not run at all; a probe that fails to send or that
// draws an ICMP error answer is an Outcome with StatusError.
type Pinger interface {
	Probe(ctx context.Context, address netip.Addr, count int, timeout time.Duration) ([]Outcome, error)
	Method() string
}

// Capability is Detect's answer: the method the process can use, or why it
// can use none.
type Capability struct {
	Method    string
	Available bool
	Reason    string
	pinger    Pinger
}

// Pinger returns the detected pinger, nil when unavailable.
func (c Capability) Pinger() Pinger { return c.pinger }

// WithPinger is an available Capability around p, for tests that replace
// Detect and for any caller that already holds a pinger.
func WithPinger(p Pinger) Capability {
	return Capability{Method: p.Method(), Available: true, pinger: p}
}

// Options say which methods the executor's configuration allows:
// network.ping-socket and
// network.ping-system, both true by default.
type Options struct {
	Socket bool
	System bool
}

// DefaultOptions allows both methods.
var DefaultOptions = Options{Socket: true, System: true}

// Detect chooses the method once per job among the allowed
// ones: both ping sockets open gives MethodSocket; otherwise a ping
// executable on PATH gives MethodSystem; otherwise the result is
// unavailable and names every cause. Detection sends no packet. Tests
// replace the variable.
var Detect = detect

func detect(opts Options) Capability {
	var causes []string
	if opts.Socket {
		sockErr := probeSockets()
		if sockErr == nil {
			return Capability{Method: MethodSocket, Available: true, pinger: socketPinger{}}
		}
		causes = append(causes, fmt.Sprintf("ICMP sockets refused (%v; net.ipv4.ping_group_range=%s)", sockErr, pingGroupRange()))
	} else {
		causes = append(causes, "the ping-socket method is disabled (network.ping-socket=false)")
	}
	if opts.System {
		if path, err := exec.LookPath("ping"); err == nil {
			return Capability{Method: MethodSystem, Available: true, pinger: systemPinger{path: path}}
		}
		causes = append(causes, "no ping executable on PATH")
	} else {
		causes = append(causes, "the system ping method is disabled (network.ping-system=false)")
	}
	return Capability{Method: MethodUnavailable, Reason: strings.Join(causes, " and ")}
}

func pingGroupRange() string {
	b, err := os.ReadFile("/proc/sys/net/ipv4/ping_group_range")
	if err != nil {
		return "unreadable"
	}
	return strings.Join(strings.Fields(string(b)), " ")
}

// Replies counts the validated replies among outcomes.
func Replies(outcomes []Outcome) int {
	n := 0
	for _, o := range outcomes {
		if o.Status == StatusReply {
			n++
		}
	}
	return n
}

// Errors counts the outcomes that carry an ICMP error answer or a send
// failure.
func Errors(outcomes []Outcome) int {
	n := 0
	for _, o := range outcomes {
		if o.Status == StatusError {
			n++
		}
	}
	return n
}

// Family is ipv4 or ipv6 for the address.
func Family(a netip.Addr) string {
	if a.Unmap().Is4() {
		return "ipv4"
	}
	return "ipv6"
}

func rtt(d time.Duration) *int64 {
	ns := d.Nanoseconds()
	return &ns
}

// OptionsFrom reads the two method switches from a configuration snapshot.
func OptionsFrom(cfg interface{ Bool(string) bool }) Options {
	return Options{Socket: cfg.Bool("network.ping-socket"), System: cfg.Bool("network.ping-system")}
}
