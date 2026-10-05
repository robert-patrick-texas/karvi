// Command karvi-fake-device serves the fake SSH device for the parity suite
// and manual runs of both transports. It prints the listening port on
// stdout; on stderr every received input line and exec request, PTY
// request, key login, signal request, and command left running as it
// arrives, and at the end the counts and the channels per connection.
package main

import (
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/fakedevice"
)

func main() {
	persona := flag.String("persona", "iosxe", "iosxe, or linux: exec requests answered from a table and a bash-like shell")
	hostname := flag.String("hostname", "", "prompt stem; default Router, or fake under -persona linux")
	user := flag.String("user", "netops", "accepted username")
	password := flag.String("password", "pw", "accepted password")
	enable := flag.String("enable", "en", "enable secret; empty means none")
	slow := flag.Duration("slow", 3*time.Second, "delay before 'show slow' answers, and the Linux persona's 'slow'")
	loginDelay := flag.Duration("login-delay", 0, "delay before the shell's first prompt")
	enableDelay := flag.Duration("enable-delay", 0, "delay before the Password: prompt that answers 'enable'")
	secretDelay := flag.Duration("secret-delay", 0, "delay before the answer to the enable secret")
	echoSecret := flag.Bool("echo-secret", false, "echo the enable secret as it is typed (a misconfigured device; karvi must still never write it)")
	big := flag.Int("big-lines", 1000, "lines 'show big' produces")
	hostKeyFile := flag.String("host-key-file", "", "file holding the ed25519 host key seed; created when absent, so a restart presents the same key")
	extra := flag.String("extra-host-keys", "", "comma-separated extra host key types (ecdsa256, ecdsa384, ecdsa521, rsa)")
	rsaSHA1Only := flag.Bool("rsa-sha1-only", false, "serve one RSA host key signing only with ssh-rsa (a legacy device)")
	kex := flag.String("kex", "", "comma-separated key exchanges the server offers (a device with only those)")
	ciphers := flag.String("ciphers", "", "comma-separated ciphers the server offers")
	macs := flag.String("macs", "", "comma-separated MACs the server offers")
	startPrivileged := flag.Bool("start-privileged", false, "start the shell at the privilege-exec prompt (a device that delivers privilege 15 at login)")
	unsaved := flag.Bool("unsaved", false, "start each shell with the running configuration modified: 'reload' asks to save first, 'copy running-config startup-config' clears it")
	port := flag.Int("port", 0, "127.0.0.1 port to listen on; 0 takes a free one (a restart that is to be the same device, port included)")
	authorizedKeys := flag.String("authorized-keys", "", "authorized_keys file whose keys log in as -user beside the password")
	noKbd := flag.Bool("no-keyboard-interactive", false, "refuse the keyboard-interactive method (a server that takes the password method alone)")
	sudoAsks := flag.Bool("sudo-asks", false, "the Linux persona's sudo needs a password: 'sudo -n id -u' is refused")
	decorations := flag.Bool("decorations", false, "the Linux persona's shell writes bash's decorations (bracketed paste, title, colours)")
	flag.Parse()
	opts := fakedevice.Options{Persona: *persona, SudoAsks: *sudoAsks, Decorations: *decorations, Port: *port, Hostname: *hostname, Username: *user, Password: *password, Enable: *enable, Delay: map[string]time.Duration{"show slow": *slow, "slow": *slow, "enable": *enableDelay}, LoginDelay: *loginDelay, SecretDelay: *secretDelay, EchoSecret: *echoSecret, BigLines: *big, RSASHA1Only: *rsaSHA1Only, StartPrivileged: *startPrivileged, Unsaved: *unsaved, NoKeyboardInteractive: *noKbd}
	split := func(v string) []string {
		if v == "" {
			return nil
		}
		return strings.Split(v, ",")
	}
	opts.KeyExchanges, opts.Ciphers, opts.MACs = split(*kex), split(*ciphers), split(*macs)
	if *extra != "" {
		opts.ExtraHostKeys = strings.Split(*extra, ",")
	}
	if *authorizedKeys != "" {
		data, err := os.ReadFile(*authorizedKeys)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		opts.AuthorizedKeys = data
	}
	if *hostKeyFile != "" {
		seed, err := loadSeed(*hostKeyFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		opts.HostKeySeed = seed
	}
	srv, err := fakedevice.Start(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(srv.Port())
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	seenLines, seenPTYs, seenKeys, seenSignals, seenLeft := 0, 0, 0, 0, 0
	report := func() {
		lines := srv.Lines()
		for _, l := range lines[seenLines:] {
			fmt.Fprintf(os.Stderr, "line: %q\n", l)
		}
		seenLines = len(lines)
		ptys := srv.PTYRequests()
		for _, p := range ptys[seenPTYs:] {
			fmt.Fprintf(os.Stderr, "pty: term=%q columns=%d rows=%d mode_bytes=%d\n", p.Term, p.Columns, p.Rows, p.ModeBytes)
		}
		seenPTYs = len(ptys)
		keys := srv.KeyLogins()
		for _, k := range keys[seenKeys:] {
			fmt.Fprintf(os.Stderr, "key: %s\n", k)
		}
		seenKeys = len(keys)
		signals := srv.Signals()
		for _, name := range signals[seenSignals:] {
			fmt.Fprintf(os.Stderr, "signal: %s\n", name)
		}
		seenSignals = len(signals)
		left := srv.LeftRunning()
		for _, l := range left[seenLeft:] {
			fmt.Fprintf(os.Stderr, "left running: %q\n", l)
		}
		seenLeft = len(left)
	}
	for {
		select {
		case <-stop:
			report()
			fmt.Fprintf(os.Stderr, "connections=%d sessions=%d\n", srv.Connections(), srv.Sessions())
			channels := make([]string, 0)
			for _, n := range srv.Channels() {
				channels = append(channels, strconv.Itoa(n))
			}
			fmt.Fprintf(os.Stderr, "channels=%s\n", strings.Join(channels, ","))
			srv.Close()
			return
		case <-time.After(100 * time.Millisecond):
			report()
		}
	}
}

func loadSeed(path string) ([]byte, error) {
	seed, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		seed = make([]byte, 32)
		if _, err := rand.Read(seed); err != nil {
			return nil, err
		}
		return seed, os.WriteFile(path, seed, 0o600)
	}
	return seed, err
}
