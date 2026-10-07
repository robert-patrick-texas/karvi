package fakedevice

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// The Linux persona: exec requests answered from a fixed table, and a
// bash-like shell for linux_shell over the same table. Its outputs use
// documentation addresses (192.0.2.0/24, 2001:db8::/32); ip's trailing
// blanks are kept, as the real command writes them.

// linuxResult is what one command of the table does.
type linuxResult struct {
	stdout, stderr string
	status         uint32
	signal         string // ends by this signal and sends no status
	noStatus       bool   // the channel closes with neither status nor signal
	slow           bool   // runs for Delay["slow"] unless a signal or the channel's close ends it first
	unknown        string // the command not found
}

// linuxCommand is the table: the built-in collection list, the cases a suite
// sends, and any other command not found.
func (s *Server) linuxCommand(text string) linuxResult {
	text = strings.TrimSpace(text)
	switch text {
	case "":
		return linuxResult{}
	case "cat /etc/os-release":
		return linuxResult{stdout: `PRETTY_NAME="Ubuntu 24.04.1 LTS"
NAME="Ubuntu"
VERSION_ID="24.04"
VERSION="24.04.1 LTS (Noble Numbat)"
VERSION_CODENAME=noble
ID=ubuntu
ID_LIKE=debian
`}
	case "uname -snrm":
		return linuxResult{stdout: "Linux " + s.opts.Hostname + " 6.8.0-0-generic x86_64\n"}
	case "ip -br address":
		return linuxResult{stdout: "lo               UNKNOWN        127.0.0.1/8 ::1/128 \n" +
			"eth0             UP             192.0.2.10/24 2001:db8::10/64 fe80::1/64 \n"}
	case "ip route show table all":
		return linuxResult{stdout: "default via 192.0.2.1 dev eth0 proto static \n" +
			"192.0.2.0/24 dev eth0 proto kernel scope link src 192.0.2.10 \n" +
			"local 127.0.0.0/8 dev lo table local proto kernel scope host src 127.0.0.1 \n" +
			"local 192.0.2.10 dev eth0 table local proto kernel scope host src 192.0.2.10 \n" +
			"2001:db8::/64 dev eth0 proto kernel metric 256 pref medium\n" +
			"default via 2001:db8::1 dev eth0 proto static metric 1024 pref medium\n"}
	case "systemctl list-unit-files --state=enabled --no-pager --no-legend":
		return linuxResult{stdout: "cron.service                 enabled enabled\n" +
			"ssh.service                  enabled enabled\n" +
			"systemd-networkd.service     enabled enabled\n"}
	case "both":
		return linuxResult{stdout: "to stdout\n", stderr: "to stderr\n"}
	case "big":
		return linuxResult{stdout: s.bigText()}
	case "bigerr":
		return linuxResult{stderr: s.bigText()}
	case "slow":
		return linuxResult{stdout: "slow output\n", slow: true}
	case "nostatus":
		return linuxResult{noStatus: true}
	case "sudo -n id -u":
		if s.opts.SudoAsks {
			return linuxResult{stderr: "sudo: a password is required\n", status: 1}
		}
		return linuxResult{stdout: "0\n"}
	}
	fields := strings.Fields(text)
	if len(fields) == 2 && fields[0] == "fail" {
		if n, err := strconv.ParseUint(fields[1], 10, 32); err == nil {
			return linuxResult{stderr: fmt.Sprintf("failing with %d\n", n), status: uint32(n % 256)}
		}
	}
	if len(fields) == 2 && fields[0] == "signal" {
		return linuxResult{signal: fields[1]}
	}
	return linuxResult{stderr: "sh: 1: " + fields[0] + ": not found\n", status: 127, unknown: fields[0]}
}

func (s *Server) bigText() string {
	var sb strings.Builder
	for i := 1; i <= s.opts.BigLines; i++ {
		fmt.Fprintf(&sb, "line %06d %s\n", i, strings.Repeat("x", 60))
	}
	return sb.String()
}

// exec runs one exec request: stdout, then stderr, then the status (or the
// signal), then the end of output, then the channel's close. sshd sends the
// end of output first, but it sends the status even after the client's
// close, and OpenSSH's client closes as soon as the output ends; x/crypto
// answers that close at once and sends nothing after it, so a status sent
// after the end of output could be lost. A slow command waits first: a
// signal request ends it by that signal, and a channel closed under it
// leaves it running, recorded.
func (s *Server) exec(ch ssh.Channel, line string, signals <-chan string, gone <-chan struct{}) {
	defer ch.Close()
	r := s.linuxCommand(line)
	if r.slow {
		timer := time.NewTimer(s.opts.Delay["slow"])
		defer timer.Stop()
		select {
		case <-timer.C:
		case name := <-signals:
			r = linuxResult{signal: name}
		case <-gone:
			s.mu.Lock()
			s.leftRunning = append(s.leftRunning, line)
			s.mu.Unlock()
			return
		}
	}
	if _, err := io.WriteString(ch, r.stdout); err != nil {
		return
	}
	if _, err := io.WriteString(ch.Stderr(), r.stderr); err != nil {
		return
	}
	switch {
	case r.noStatus:
	case r.signal != "":
		ch.SendRequest("exit-signal", false, ssh.Marshal(struct {
			Signal     string
			CoreDumped bool
			Error      string
			Lang       string
		}{r.signal, false, "", ""}))
	default:
		ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{r.status}))
	}
	ch.CloseWrite()
}

// linuxPrompt is the shell's prompt, with bash's decorations under
// Options.Decorations: bracketed paste on, the window title, the colours.
func (s *Server) linuxPrompt() string {
	who := s.opts.Username + "@" + s.opts.Hostname
	if !s.opts.Decorations {
		return who + ":~$ "
	}
	return "\x1b[?2004h\x1b]0;" + who + ": ~\x07\x1b[01;32m" + who + "\x1b[00m:\x1b[01;34m~\x1b[00m$ "
}

// linuxShell is the shell for linux_shell: each line echoed as typed and
// answered from the table, both streams on the terminal, the newlines as a
// terminal's; a command not found is bash's message. exit or logout ends it
// with status 0, as a login shell does.
func (s *Server) linuxShell(ch ssh.Channel) {
	defer ch.Close()
	w := func(text string) bool {
		_, err := io.WriteString(ch, text)
		return err == nil
	}
	time.Sleep(s.opts.LoginDelay)
	if !w(s.linuxPrompt()) {
		return
	}
	reader := bufio.NewReader(ch)
	var line []byte
	lastCR := false
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return
		}
		if b == '\n' && lastCR {
			lastCR = false
			continue
		}
		lastCR = b == '\r'
		if b != '\n' && b != '\r' {
			line = append(line, b)
			if !w(string(b)) { // the terminal echo
				return
			}
			continue
		}
		text := string(line)
		line = line[:0]
		s.record(text)
		// bash ends the line and, decorated, turns bracketed paste off.
		end := "\r\n"
		if s.opts.Decorations {
			end += "\x1b[?2004l\r"
		}
		if !w(end) {
			return
		}
		switch strings.TrimSpace(text) {
		case "exit", "logout":
			w("logout\r\n")
			ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
			return
		}
		time.Sleep(s.opts.Delay[strings.TrimSpace(text)])
		r := s.linuxCommand(text)
		if r.unknown != "" {
			r.stderr = "-bash: " + r.unknown + ": command not found\n"
		}
		if !w(strings.ReplaceAll(r.stdout+r.stderr, "\n", "\r\n") + s.linuxPrompt()) {
			return
		}
	}
}
