package icmpgate

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakePing installs a ping executable first on PATH whose stdout, stderr,
// and exit code are fixed, and that sleeps for hang before exiting.
func fakePing(t *testing.T, stdout, stderr string, exit int, hang time.Duration) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ping")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' %s >&2\nsleep %s\nprintf '%%s' %s\nexit %d\n", shellQuote(stderr), fmt.Sprintf("%.3f", hang.Seconds()), shellQuote(stdout), exit)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return path
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

const (
	twoReplies = "PING 127.0.0.1 (127.0.0.1) 56(84) bytes of data.\n64 bytes from 127.0.0.1: icmp_seq=1 ttl=64 time=0.052 ms\n64 bytes from 127.0.0.1: icmp_seq=2 ttl=64 time=0.051 ms\n\n--- 127.0.0.1 ping statistics ---\n2 packets transmitted, 2 received, 0% packet loss, time 508ms\n"
	twoMisses  = "PING 192.0.2.1 (192.0.2.1) 56(84) bytes of data.\nno answer yet for icmp_seq=1\n\n--- 192.0.2.1 ping statistics ---\n2 packets transmitted, 0 received, 100% packet loss, time 507ms\n"
	v6Errors   = "PING 2001:db8::1 (2001:db8::1) 56 data bytes\nFrom 2001:668:0:3:ffff:2:0:7c1 icmp_seq=1 Destination unreachable: No route\nno answer yet for icmp_seq=1\nFrom 2001:668:0:3:ffff:2:0:7c1 icmp_seq=2 Destination unreachable: No route\n\n--- 2001:db8::1 ping statistics ---\n2 packets transmitted, 0 received, +2 errors, 100% packet loss, time 503ms\n"
	oneReply   = "PING 10.0.0.1 (10.0.0.1) 56(84) bytes of data.\n64 bytes from 10.0.0.1: icmp_seq=1 ttl=64 time=2.100 ms\nno answer yet for icmp_seq=2\n\n--- 10.0.0.1 ping statistics ---\n2 packets transmitted, 1 received, 50% packet loss, time 1004ms\n"
)

func TestSystemAdapterParsesTheThreeLineShapes(t *testing.T) {
	cases := []struct {
		name, stdout, stderr string
		exit                 int
		address              string
		want                 [2]string
		replies, errs        int
		detail               string
	}{
		{"two replies", twoReplies, "", 0, "127.0.0.1", [2]string{StatusReply, StatusReply}, 2, 0, ""},
		{"two misses", twoMisses, "", 1, "192.0.2.1", [2]string{StatusTimeout, StatusTimeout}, 0, 0, ""},
		{"icmp errors", v6Errors, "", 1, "2001:db8::1", [2]string{StatusError, StatusError}, 0, 2, "Destination unreachable: No route"},
		{"one reply", oneReply, "", 1, "10.0.0.1", [2]string{StatusReply, StatusTimeout}, 1, 0, ""},
		{"adapter failure", "", "ping: 256.1.1.1: Name or service not known\n", 2, "192.0.2.2", [2]string{StatusError, StatusError}, 0, 2, "256.1.1.1: Name or service not known"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := fakePing(t, c.stdout, c.stderr, c.exit, 0)
			p := systemPinger{path: path}
			out, err := p.Probe(context.Background(), netip.MustParseAddr(c.address), Probes, 500*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			if len(out) != 2 {
				t.Fatalf("outcomes %d", len(out))
			}
			for i, o := range out {
				if o.Sequence != i+1 || o.Status != c.want[i] {
					t.Errorf("outcome %d: %+v, want %s", i+1, o, c.want[i])
				}
				if o.Status == StatusReply && (o.RTTNS == nil || *o.RTTNS <= 0 || o.From == "") {
					t.Errorf("reply %d without rtt or source: %+v", i+1, o)
				}
				if c.detail != "" && o.Detail != c.detail {
					t.Errorf("outcome %d detail %q, want %q", i+1, o.Detail, c.detail)
				}
			}
			if Replies(out) != c.replies || Errors(out) != c.errs {
				t.Errorf("replies %d errors %d, want %d and %d", Replies(out), Errors(out), c.replies, c.errs)
			}
		})
	}
}

func TestSystemAdapterReplyRTTAndSource(t *testing.T) {
	path := fakePing(t, oneReply, "", 1, 0)
	out, err := systemPinger{path: path}.Probe(context.Background(), netip.MustParseAddr("10.0.0.1"), Probes, 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if *out[0].RTTNS != int64(2100*time.Microsecond) || out[0].From != "10.0.0.1" {
		t.Errorf("reply: %+v", out[0])
	}
}

func TestSystemAdapterKillsAHungProcess(t *testing.T) {
	path := fakePing(t, twoReplies, "", 0, 5*time.Second)
	started := time.Now()
	out, err := systemPinger{path: path}.Probe(context.Background(), netip.MustParseAddr("127.0.0.1"), Probes, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Errorf("adapter waited %s for a hung process", elapsed)
	}
	for _, o := range out {
		if o.Status != StatusError || !strings.Contains(o.Detail, "deadline") {
			t.Errorf("outcome: %+v", o)
		}
	}
}

func TestSystemAdapterHonoursCancellation(t *testing.T) {
	path := fakePing(t, twoReplies, "", 0, 5*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	out, err := systemPinger{path: path}.Probe(ctx, netip.MustParseAddr("127.0.0.1"), Probes, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range out {
		if o.Status != StatusError || o.Detail != context.Canceled.Error() {
			t.Errorf("outcome after cancel: %+v", o)
		}
	}
}

func TestSystemAdapterAgainstTheRealExecutable(t *testing.T) {
	real := "/usr/bin/ping"
	if _, err := os.Stat(real); err != nil {
		t.Skip("no /usr/bin/ping on this host")
	}
	out, err := systemPinger{path: real}.Probe(context.Background(), netip.MustParseAddr("127.0.0.1"), Probes, 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if Replies(out) != 2 {
		t.Skipf("the real ping did not answer for loopback (%+v); capability, not parsing", out)
	}
	for _, o := range out {
		if o.From != "127.0.0.1" || *o.RTTNS <= 0 {
			t.Errorf("outcome: %+v", o)
		}
	}
}

func TestSocketPingerLoopbackOrSkip(t *testing.T) {
	if err := probeSockets(); err != nil {
		t.Skipf("ping sockets refused on this host (%v; net.ipv4.ping_group_range=%s)", err, pingGroupRange())
	}
	for _, addr := range []string{"127.0.0.1", "::1"} {
		started := time.Now()
		out, err := socketPinger{}.Probe(context.Background(), netip.MustParseAddr(addr), Probes, 500*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		if Replies(out) != 2 || time.Since(started) > 500*time.Millisecond {
			t.Errorf("%s: %+v in %s", addr, out, time.Since(started))
		}
	}
	// TEST-NET-1 (RFC 5737) is never answered: two independent timeouts.
	started := time.Now()
	out, err := socketPinger{}.Probe(context.Background(), netip.MustParseAddr("192.0.2.1"), Probes, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if Replies(out) != 0 || time.Since(started) < 200*time.Millisecond || time.Since(started) > time.Second {
		t.Errorf("unreachable: %+v in %s", out, time.Since(started))
	}
}

func TestEchoMessageAndMatching(t *testing.T) {
	payload := []byte(strings.Repeat("p", payloadBytes))
	msg := echoMessage(icmpv4EchoRequest, 2, payload, true)
	if msg[0] != icmpv4EchoRequest || binary.BigEndian.Uint16(msg[6:8]) != 2 || len(msg) != 8+payloadBytes {
		t.Fatalf("message %x", msg)
	}
	if checksum(msg) != 0 {
		t.Errorf("checksum over a checksummed message is %#x, want 0", checksum(msg))
	}
	reply := append([]byte{}, msg...)
	reply[0] = icmpv4EchoReply
	if !matches(reply, icmpv4EchoReply, 2, payload) {
		t.Error("own reply does not match")
	}
	if matches(reply, icmpv4EchoReply, 1, payload) {
		t.Error("sequence 1 matched a sequence 2 reply")
	}
	other := append([]byte{}, reply...)
	other[8] ^= 1
	if matches(other, icmpv4EchoReply, 2, payload) {
		t.Error("foreign payload matched")
	}
	if matches(msg, icmpv4EchoReply, 2, payload) {
		t.Error("a request matched as a reply")
	}
}

func TestDetectOrder(t *testing.T) {
	saved := probeSockets
	defer func() { probeSockets = saved }()
	probeSockets = func() error { return nil }
	c := Detect(DefaultOptions)
	if c.Method != MethodSocket || !c.Available || c.Pinger() == nil || c.Pinger().Method() != MethodSocket {
		t.Errorf("sockets open: %+v", c)
	}
	probeSockets = func() error { return fmt.Errorf("ipv4 ping socket: permission denied") }
	fakePing(t, twoReplies, "", 0, 0)
	c = Detect(DefaultOptions)
	if c.Method != MethodSystem || !c.Available || c.Pinger() == nil || c.Pinger().Method() != MethodSystem {
		t.Errorf("sockets refused, ping on PATH: %+v", c)
	}
	t.Setenv("PATH", t.TempDir())
	c = Detect(DefaultOptions)
	if c.Method != MethodUnavailable || c.Available || c.Pinger() != nil {
		t.Errorf("sockets refused, no ping: %+v", c)
	}
	for _, want := range []string{"permission denied", "ping_group_range=", "no ping executable on PATH"} {
		if !strings.Contains(c.Reason, want) {
			t.Errorf("reason %q lacks %q", c.Reason, want)
		}
	}
}

func TestDetectHonoursTheMethodOptions(t *testing.T) {
	saved := probeSockets
	defer func() { probeSockets = saved }()
	probeSockets = func() error { return nil }
	fakePing(t, twoReplies, "", 0, 0)
	if c := Detect(Options{Socket: false, System: true}); c.Method != MethodSystem {
		t.Errorf("socket disabled: %+v", c)
	}
	if c := Detect(Options{Socket: true, System: false}); c.Method != MethodSocket {
		t.Errorf("system disabled, sockets open: %+v", c)
	}
	probeSockets = func() error { return fmt.Errorf("refused") }
	c := Detect(Options{Socket: true, System: false})
	if c.Available || !strings.Contains(c.Reason, "network.ping-system=false") || !strings.Contains(c.Reason, "refused") {
		t.Errorf("system disabled, sockets refused: %+v", c)
	}
	c = Detect(Options{})
	if c.Available || !strings.Contains(c.Reason, "network.ping-socket=false") || !strings.Contains(c.Reason, "network.ping-system=false") {
		t.Errorf("both disabled: %+v", c)
	}
}

func TestSocketProbeMatchesTheHost(t *testing.T) {
	// Whatever this host allows, the real probe and the real Detect agree.
	err := openSockets()
	c := detect(DefaultOptions)
	if (err == nil) != (c.Method == MethodSocket) {
		t.Errorf("openSockets err=%v but Detect method %s", err, c.Method)
	}
	t.Logf("host: net.ipv4.ping_group_range=%s method=%s", pingGroupRange(), c.Method)
}

func TestFamilyAndCounts(t *testing.T) {
	if Family(netip.MustParseAddr("::ffff:10.0.0.1")) != "ipv4" || Family(netip.MustParseAddr("2001:db8::1")) != "ipv6" {
		t.Error("family")
	}
	out := []Outcome{{Status: StatusReply}, {Status: StatusError}}
	if Replies(out) != 1 || Errors(out) != 1 {
		t.Error("counts")
	}
}
