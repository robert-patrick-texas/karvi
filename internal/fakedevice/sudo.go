package fakedevice

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// The Linux persona's sudo, as EXAMPLES chapter 96's container showed it
// (ex96-sudo-container, ex96d-stderr, ex96e-unseen: sudo.ws 1.9.17p2 and
// sudo-rs 0.2.13, Ubuntu 26.04's, read with stderr alone): classic sudo's
// prompt and messages, or sudo-rs's under Options.SudoRS. Without
// Options.SudoAsks every command runs as root with no password asked, as
// under a NOPASSWD rule. With it sudo wants Options.Enable, or the login
// password when Enable is empty (sudo's own default, the user's password):
// sudo -n refuses; on an exec channel, with no terminal, sudo without -S
// refuses; with -S it writes its prompt on stderr and reads a line of the
// channel's input, up to three tries, as sudo does.

// sudoCall is a sudo command line as the persona reads it: sudo, the
// options -n, -S, and -p PROMPT (one word, a single-quoted part taken
// without its quotes as sh takes it, no % escape expanded), an optional
// --, and the command, the rest of the line as written.
type sudoCall struct {
	nonInteractive bool   // -n
	stdin          bool   // -S
	prompt         string // -p's argument; empty for sudo's own prompt
	command        string
	// usage: an option the persona does not know, a quote left open, or
	// no command.
	usage bool
}

// parseSudo reads line as a sudo command line; false when it does not run
// sudo.
func parseSudo(line string) (sudoCall, bool) {
	rest := strings.TrimSpace(line)
	if rest != "sudo" && !strings.HasPrefix(rest, "sudo ") {
		return sudoCall{}, false
	}
	rest = strings.TrimPrefix(rest, "sudo")
	var c sudoCall
	for {
		rest = strings.TrimLeft(rest, " ")
		word, after, ok := cutWord(rest)
		switch {
		case !ok:
			c.usage = true
			return c, true
		case word == "-n":
			c.nonInteractive = true
		case word == "-S":
			c.stdin = true
		case word == "-p":
			if c.prompt, after, ok = cutWord(strings.TrimLeft(after, " ")); !ok {
				c.usage = true
				return c, true
			}
		case word == "--":
			c.command = strings.TrimLeft(after, " ")
			c.usage = c.command == ""
			return c, true
		case strings.HasPrefix(word, "-"):
			c.usage = true
			return c, true
		default:
			c.command = rest
			c.usage = rest == ""
			return c, true
		}
		rest = after
	}
}

// cutWord takes the first word of s, up to a space, a single-quoted part
// taken without its quotes; ok is false for a quote left open.
func cutWord(s string) (word, rest string, ok bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ':
			return b.String(), s[i:], true
		case '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return "", "", false
			}
			b.WriteString(s[i+1 : i+1+j])
			i += j + 1
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String(), "", true
}

// sudoPassword is the password the persona's sudo takes under SudoAsks.
func (s *Server) sudoPassword() string {
	if s.opts.Enable != "" {
		return s.opts.Enable
	}
	return s.opts.Password
}

// sudo runs a sudo command line. in is the exec channel's input and errOut
// its stderr, where each prompt, sudo-rs's line end after each read, and
// sudo's answer to each wrong try go as they happen; sudo's last message
// and the command's output are the result's. Each password line read is
// recorded as "<secret>". The shell gives neither: its sudo reads no
// password (--sudo is the exec channel's alone), so a password wanted there
// is sudo -n's refusal.
func (s *Server) sudo(c sudoCall, in *bufio.Reader, errOut io.Writer) linuxResult {
	rs := s.opts.SudoRS
	refuse := func(classic, sudoRS string) linuxResult {
		if rs {
			return linuxResult{stderr: sudoRS, status: 1}
		}
		return linuxResult{stderr: classic, status: 1}
	}
	if c.usage {
		return linuxResult{stderr: "usage: sudo [-n] [-S] [-p prompt] [--] command (the fake's sudo)\n", status: 1}
	}
	if s.opts.SudoAsks {
		switch {
		case c.nonInteractive || in == nil:
			return refuse("sudo: a password is required\n", "sudo: interactive authentication is required\n")
		case !c.stdin:
			return refuse("sudo: a terminal is required to read the password; either use the -S option to read from standard input or configure an askpass helper\nsudo: a password is required\n",
				"sudo: A terminal is required to authenticate\n")
		}
		prompt := c.prompt
		switch {
		case rs && prompt == "":
			prompt = "[sudo: authenticate] Password: "
		case rs:
			prompt = "[sudo: " + prompt + "] Password: "
		case prompt == "":
			prompt = "[sudo] password for " + s.opts.Username + ": "
		}
		for wrong := 0; ; {
			_, _ = io.WriteString(errOut, prompt)
			line, err := in.ReadString('\n')
			if rs {
				_, _ = io.WriteString(errOut, "\n")
			}
			if line == "" && err != nil {
				// The input's end: classic sudo ends its prompt's line and
				// names the wrong tries, if any.
				last := "sudo: a password is required\n"
				switch {
				case wrong == 1:
					last = "sudo: 1 incorrect password attempt\n"
				case wrong > 1:
					last = fmt.Sprintf("sudo: %d incorrect password attempts\n", wrong)
				}
				return refuse("\nsudo: no password was provided\n"+last, "sudo: Authentication required but not attempted\n")
			}
			s.record("<secret>")
			if strings.TrimSuffix(line, "\n") == s.sudoPassword() {
				break
			}
			if wrong++; wrong == 3 {
				return refuse("sudo: 3 incorrect password attempts\n", "sudo: maximum 3 incorrect authentication attempts\n")
			}
			if rs {
				_, _ = io.WriteString(errOut, "sudo: Authentication failed, try again.\n")
			} else {
				_, _ = io.WriteString(errOut, "Sorry, try again.\n")
			}
		}
	}
	r := s.linuxCommand(c.command, true)
	if r.unknown != "" {
		if rs {
			return linuxResult{stderr: "sudo: '" + r.unknown + "': command not found\n", status: 1}
		}
		return linuxResult{stderr: "sudo: " + r.unknown + ": command not found\n", status: 1}
	}
	return r
}
