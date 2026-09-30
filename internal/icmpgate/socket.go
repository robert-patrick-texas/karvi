package icmpgate

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"syscall"
	"time"
)

// socketPinger sends the probes over Linux ping sockets (SOCK_DGRAM with
// IPPROTO_ICMP or IPPROTO_ICMPV6), which the kernel grants to the groups in
// net.ipv4.ping_group_range without any capability. The kernel assigns the
// identifier and demultiplexes replies to the socket; the pinger matches
// sequence and payload as well. The loop is the gate's: one probe
// in flight, an independent timeout per probe, so a late reply for sequence
// 1 arriving in sequence 2's window is discarded and sequence 1 stays a
// timeout.
type socketPinger struct{}

func (socketPinger) Method() string { return MethodSocket }

const (
	icmpv4EchoRequest = 8
	icmpv4EchoReply   = 0
	icmpv6EchoRequest = 128
	icmpv6EchoReply   = 129
	payloadBytes      = 32 // 16-byte nonce plus 16 bytes of the target digest
)

// probeSockets opens and closes one ping socket per family (Detect); tests
// replace it to reach every detection branch on any host.
var probeSockets = openSockets

func openSockets() error {
	for _, f := range []struct {
		domain, proto int
		name          string
	}{{syscall.AF_INET, syscall.IPPROTO_ICMP, "ipv4"}, {syscall.AF_INET6, syscall.IPPROTO_ICMPV6, "ipv6"}} {
		fd, err := syscall.Socket(f.domain, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, f.proto)
		if err != nil {
			return fmt.Errorf("%s ping socket: %w", f.name, err)
		}
		syscall.Close(fd)
	}
	return nil
}

func openSocket(address netip.Addr) (net.PacketConn, error) {
	domain, proto := syscall.AF_INET, syscall.IPPROTO_ICMP
	if !address.Unmap().Is4() {
		domain, proto = syscall.AF_INET6, syscall.IPPROTO_ICMPV6
	}
	fd, err := syscall.Socket(domain, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC|syscall.SOCK_NONBLOCK, proto)
	if err != nil {
		return nil, fmt.Errorf("ping socket: %w", err)
	}
	f := os.NewFile(uintptr(fd), "icmp")
	conn, err := net.FilePacketConn(f)
	f.Close() // FilePacketConn duplicated the descriptor
	if err != nil {
		return nil, fmt.Errorf("ping socket: %w", err)
	}
	return conn, nil
}

// Probe runs the loop; callers pass a validated count and timeout
// (executionplan.PingSettings refuses anything but 2 and a positive value).
func (p socketPinger) Probe(ctx context.Context, address netip.Addr, count int, timeout time.Duration) ([]Outcome, error) {
	conn, err := openSocket(address)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.SetDeadline(time.Now()) })
	defer stop()

	address = address.Unmap()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	digest := sha256.Sum256([]byte(address.String()))
	payload := append(append([]byte{}, nonce[:]...), digest[:16]...)
	dst := &net.UDPAddr{IP: address.AsSlice()}
	request, reply := byte(icmpv4EchoRequest), byte(icmpv4EchoReply)
	if address.Is6() {
		request, reply = icmpv6EchoRequest, icmpv6EchoReply
	}
	outcomes := make([]Outcome, 0, count)
	buf := make([]byte, 1500)
	for seq := 1; seq <= count; seq++ {
		msg := echoMessage(request, uint16(seq), payload, address.Is4())
		sent := time.Now()
		deadline := sent.Add(timeout)
		o := Outcome{Sequence: seq, SentAt: sent, Status: StatusTimeout}
		if ctx.Err() != nil {
			o.Status, o.Detail = StatusError, ctx.Err().Error()
			outcomes = append(outcomes, o)
			continue
		}
		conn.SetWriteDeadline(deadline)
		if _, err := conn.WriteTo(msg, dst); err != nil {
			o.Status, o.Detail = StatusError, err.Error()
			outcomes = append(outcomes, o)
			continue
		}
		conn.SetReadDeadline(deadline)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				if ctx.Err() != nil {
					o.Status, o.Detail = StatusError, ctx.Err().Error()
				} else if !errors.Is(err, os.ErrDeadlineExceeded) {
					o.Status, o.Detail = StatusError, err.Error()
				}
				break
			}
			if matches(buf[:n], reply, uint16(seq), payload) {
				o.Status, o.RTTNS, o.From = StatusReply, rtt(time.Since(sent)), addrOf(from)
				break
			}
		}
		outcomes = append(outcomes, o)
	}
	return outcomes, nil
}

// echoMessage is an ICMP echo request: type, code 0, checksum, identifier
// (the kernel overwrites it with the socket's), sequence, payload. The
// checksum is filled for IPv4; the kernel computes the ICMPv6 one.
func echoMessage(typ byte, seq uint16, payload []byte, v4 bool) []byte {
	msg := make([]byte, 8+len(payload))
	msg[0] = typ
	binary.BigEndian.PutUint16(msg[6:8], seq)
	copy(msg[8:], payload)
	if v4 {
		binary.BigEndian.PutUint16(msg[2:4], checksum(msg))
	}
	return msg
}

func checksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}

// matches accepts an echo reply of the expected type whose sequence and
// payload are ours; the kernel already matched the identifier.
func matches(b []byte, reply byte, seq uint16, payload []byte) bool {
	if len(b) < 8+len(payload) || b[0] != reply {
		return false
	}
	if binary.BigEndian.Uint16(b[6:8]) != seq {
		return false
	}
	return string(b[8:8+len(payload)]) == string(payload)
}

func addrOf(a net.Addr) string {
	if u, ok := a.(*net.UDPAddr); ok {
		if ip, ok := netip.AddrFromSlice(u.IP); ok {
			return ip.Unmap().String()
		}
	}
	if a != nil {
		return a.String()
	}
	return ""
}
