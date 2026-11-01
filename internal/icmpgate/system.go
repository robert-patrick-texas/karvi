package icmpgate

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// systemPinger runs the system ping executable, the approved system ping
// adapter, for hosts that refuse ping sockets to the operator. It
// asks for numeric output, outstanding-reply reporting, count probes, and
// both the interval and the wait set to the timeout, under an environment
// of LC_ALL=C only, and parses the reply and error lines. ping sends probe
// 2 at the interval whether or not probe 1 answered, so a
// gate takes about one timeout on success and two on failure. A ping that
// cannot start, or that exits refusing its socket, a permission, or its
// arguments before answering a probe, is an error return: the pinger could
// not run.
type systemPinger struct{ path string }

func (systemPinger) Method() string { return MethodSystem }

var (
	replyLine = regexp.MustCompile(`^\d+ bytes from (\S+?):? icmp_seq=(\d+) .*time=([\d.]+) ms`)
	errorLine = regexp.MustCompile(`^From (\S+?):? icmp_seq=(\d+) (.*)$`)
)

// minInterval is the smallest interval iputils accepts from an unprivileged
// caller (2 ms since 20210202); a shorter timeout is clamped for the flag
// only, the wait keeps the operator's value.
const minInterval = 2 * time.Millisecond

// Probe runs the adapter; callers pass a validated count and timeout.
func (p systemPinger) Probe(ctx context.Context, address netip.Addr, count int, timeout time.Duration) ([]Outcome, error) {
	address = address.Unmap()
	family := "-4"
	if address.Is6() {
		family = "-6"
	}
	interval := timeout
	if interval < minInterval {
		interval = minInterval
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(count)*timeout+time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, p.path, family, "-n", "-O", "-c", strconv.Itoa(count), "-i", seconds(interval), "-W", seconds(timeout), address.String())
	cmd.Env = []string{"LC_ALL=C"}
	cmd.WaitDelay = time.Second // give up on the pipes once the process is killed
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	started := time.Now()
	runErr := cmd.Run()

	outcomes := make([]Outcome, count)
	for i := range outcomes {
		outcomes[i] = Outcome{Sequence: i + 1, SentAt: started.Add(time.Duration(i) * interval), Status: StatusTimeout}
	}
	sc := bufio.NewScanner(&stdout)
	for sc.Scan() {
		line := sc.Text()
		if m := replyLine.FindStringSubmatch(line); m != nil {
			seq, _ := strconv.Atoi(m[2])
			ms, _ := strconv.ParseFloat(m[3], 64)
			if o := outcomeAt(outcomes, seq); o != nil && o.Status == StatusTimeout {
				o.Status, o.RTTNS, o.From = StatusReply, rtt(time.Duration(ms*float64(time.Millisecond))), m[1]
			}
			continue
		}
		if m := errorLine.FindStringSubmatch(line); m != nil {
			seq, _ := strconv.Atoi(m[2])
			if o := outcomeAt(outcomes, seq); o != nil && o.Status == StatusTimeout {
				o.Status, o.From, o.Detail = StatusError, m[1], strings.TrimSpace(m[3])
			}
		}
	}
	var exit *exec.ExitError
	exited := errors.As(runErr, &exit)
	switch {
	case runCtx.Err() != nil && ctx.Err() == nil:
		markErrors(outcomes, "ping did not finish within the gate deadline")
	case ctx.Err() != nil:
		markErrors(outcomes, ctx.Err().Error())
	case runErr != nil && !exited:
		return nil, runErr
	case exited && exit.ExitCode() >= 2:
		if line, ok := cannotRun(stderr.String(), p.path); ok && Replies(outcomes) == 0 && Errors(outcomes) == 0 {
			return nil, fmt.Errorf("%s: %s", p.path, line)
		}
		detail := firstLine(stderr.String())
		if detail == "" {
			detail = runErr.Error()
		}
		markErrors(outcomes, detail)
	}
	return outcomes, nil
}

// cannotRunLine matches a line of ping's stderr, lower-cased, that says ping
// could not run at all: a socket refused, a permission or capability
// lacking, or arguments refused.
var cannotRunLine = regexp.MustCompile(`socket: |permission denied|operation not permitted|cap_net_raw|invalid option|unrecognized option|option requires an argument|invalid argument|^usage|cannot flood|^bad `)

// cannotRun is the first line of stderr, without ping's program prefix, that
// says ping could not run at all. A line about reaching the address
// (connect, sendmsg, sendto) is never one: ping ran, and the address is not
// reachable from this host.
func cannotRun(stderr, path string) (string, bool) {
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(line), path+": "), "ping: ")
		l := strings.ToLower(line)
		if strings.HasPrefix(l, "connect:") || strings.HasPrefix(l, "sendmsg:") || strings.HasPrefix(l, "sendto:") {
			continue
		}
		if cannotRunLine.MatchString(l) {
			return line, true
		}
	}
	return "", false
}

func outcomeAt(outcomes []Outcome, seq int) *Outcome {
	if seq < 1 || seq > len(outcomes) {
		return nil
	}
	return &outcomes[seq-1]
}

// markErrors turns every sequence still counted as a timeout into an error
// with detail: the adapter did not run to a conclusion for them.
func markErrors(outcomes []Outcome, detail string) {
	for i := range outcomes {
		if outcomes[i].Status == StatusTimeout {
			outcomes[i].Status, outcomes[i].Detail = StatusError, detail
		}
	}
}

func seconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 3, 64)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimPrefix(s, "ping: ")
}
