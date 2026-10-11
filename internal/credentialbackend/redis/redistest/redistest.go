// Package redistest is a RESP server for the Redis backend's tests: it
// answers AUTH, SELECT, and HGETALL from the hashes it holds, records every
// command it is sent, and can be made to accept connections and never
// answer.
package redistest

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Server is one listener on 127.0.0.1. Hashes maps a key to its fields;
// Password, when set, is the last argument AUTH must give.
type Server struct {
	Addr     string
	Hashes   map[string]map[string]string
	Password string
	// Silent accepts every connection and reads, never answering.
	Silent bool

	mu       sync.Mutex
	commands [][]string
	conns    int
}

// Start listens with cfg's TLS when it is set, plain otherwise, and closes
// the listener at the test's end.
func Start(t testing.TB, s *Server, cfg *tls.Config) *Server {
	t.Helper()
	var ln net.Listener
	var err error
	if cfg != nil {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", cfg)
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	s.Addr = ln.Addr().String()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.conns++
			s.mu.Unlock()
			go s.serve(c)
		}
	}()
	return s
}

// Commands are the commands received so far, each its arguments.
func (s *Server) Commands() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]string(nil), s.commands...)
}

// Count is how many received commands were named name.
func (s *Server) Count(name string) int {
	n := 0
	for _, c := range s.Commands() {
		if strings.EqualFold(c[0], name) {
			n++
		}
	}
	return n
}

// Conns is how many connections were accepted.
func (s *Server) Conns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conns
}

func (s *Server) serve(c net.Conn) {
	defer c.Close()
	if s.Silent {
		_, _ = io.Copy(io.Discard, c)
		return
	}
	r := bufio.NewReader(c)
	for {
		args, err := readCommand(r)
		if err != nil {
			return
		}
		s.mu.Lock()
		s.commands = append(s.commands, args)
		s.mu.Unlock()
		var reply string
		switch strings.ToUpper(args[0]) {
		case "AUTH":
			if s.Password != "" && args[len(args)-1] == s.Password {
				reply = "+OK\r\n"
			} else {
				reply = "-WRONGPASS invalid password\r\n"
			}
		case "SELECT":
			reply = "+OK\r\n"
		case "HGETALL":
			h := s.Hashes[args[1]]
			var b strings.Builder
			fmt.Fprintf(&b, "*%d\r\n", 2*len(h))
			for k, v := range h {
				fmt.Fprintf(&b, "$%d\r\n%s\r\n$%d\r\n%s\r\n", len(k), k, len(v), v)
			}
			reply = b.String()
		default:
			reply = "-ERR unknown command\r\n"
		}
		if _, err := io.WriteString(c, reply); err != nil {
			return
		}
	}
}

func readCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "*")))
	if err != nil || n < 1 {
		return nil, fmt.Errorf("not a RESP array: %q", line)
	}
	args := make([]string, n)
	for i := range args {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "$")))
		if err != nil {
			return nil, fmt.Errorf("not a bulk string: %q", line)
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args[i] = string(buf[:size])
	}
	return args, nil
}
