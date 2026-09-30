package icmpgate

import (
	"bufio"
	"bytes"
	"context"
	"errors"
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
// gate takes about one timeout on success and two on failure.
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
	switch {
	case runCtx.Err() != nil && ctx.Err() == nil:
		markErrors(outcomes, "ping did not finish within the gate deadline")
	case ctx.Err() != nil:
		markErrors(outcomes, ctx.Err().Error())
	case errors.As(runErr, &exit) && exit.ExitCode() >= 2, runErr != nil && !errors.As(runErr, &exit):
		detail := firstLine(stderr.String())
		if detail == "" {
			detail = runErr.Error()
		}
		markErrors(outcomes, detail)
	}
	return outcomes, nil
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
