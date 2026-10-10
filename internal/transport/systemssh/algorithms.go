package systemssh

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/sshalgorithms"
)

// queryKinds are OpenSSH's -Q names for the algorithm lists.
var queryKinds = map[sshalgorithms.Kind]string{
	sshalgorithms.HostKey: "HostKeyAlgorithms",
	sshalgorithms.Kex:     "kex",
	sshalgorithms.Ciphers: "cipher",
	sshalgorithms.MACs:    "mac",
}

// capabilities is what one OpenSSH binary reports it implements. A list
// the binary did not answer for (no known name in the reply) is not
// filtered: every real OpenSSH answers -Q, and a stand-in that does not
// is given the configured list as written.
type capabilities struct {
	names    map[sshalgorithms.Kind]map[string]bool
	answered map[sshalgorithms.Kind]bool
}

func (c capabilities) implements(kind sshalgorithms.Kind, name string) bool {
	if !c.answered[kind] {
		return true
	}
	return c.names[kind][name]
}

var capabilityCache = struct {
	sync.Mutex
	byBinary map[string]capabilities
}{byBinary: map[string]capabilities{}}

// binaryCapabilities queries `ssh -Q` once per binary per process
// (OpenSSH refuses a whole list for one name it lacks).
func binaryCapabilities(cfg configload.Snapshot, binary string) capabilities {
	capabilityCache.Lock()
	defer capabilityCache.Unlock()
	if c, ok := capabilityCache.byBinary[binary]; ok {
		return c
	}
	c := capabilities{names: map[sshalgorithms.Kind]map[string]bool{}, answered: map[sshalgorithms.Kind]bool{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for kind, query := range queryKinds {
		wg.Add(1)
		go func(kind sshalgorithms.Kind, query string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "-Q", query)
			cmd.Env = childEnvironment(cfg)
			out, err := cmd.Output()
			if err != nil {
				return
			}
			names := map[string]bool{}
			answered := false
			scanner := bufio.NewScanner(bytes.NewReader(out))
			for scanner.Scan() {
				name := strings.TrimSpace(scanner.Text())
				if name == "" {
					continue
				}
				names[name] = true
				if sshalgorithms.Classify(kind, name) != sshalgorithms.Unknown {
					answered = true
				}
			}
			mu.Lock()
			c.names[kind], c.answered[kind] = names, answered
			mu.Unlock()
		}(kind, query)
	}
	wg.Wait()
	capabilityCache.byBinary[binary] = c
	return c
}

var negotiationDiagnostic = regexp.MustCompile(`no matching (key exchange method|cipher|MAC|host key type) found\. Their offer: (\S+)`)

// negotiationFailure reads OpenSSH's "Unable to negotiate ... no matching X
// found. Their offer: ..." into ssh_algorithm_negotiation_failed; the host
// key case under a filtered list is the caller's (host_key_changed). A list
// offered does not hold was OpenSSH's own, which effective gives as the
// session's configuration made it.
func negotiationFailure(diagnostic string, offered sshalgorithms.Lists, effective func() sshalgorithms.Lists) (sshalgorithms.Kind, error, bool) {
	m := negotiationDiagnostic.FindStringSubmatch(diagnostic)
	if m == nil {
		return "", nil, false
	}
	kind := map[string]sshalgorithms.Kind{"key exchange method": sshalgorithms.Kex, "cipher": sshalgorithms.Ciphers, "MAC": sshalgorithms.MACs, "host key type": sshalgorithms.HostKey}[m[1]]
	offerer, list := "karvi", offered[kind]
	if _, held := offered[kind]; !held {
		offerer, list = "system", nil
		if effective != nil {
			list = effective()[kind]
		}
	}
	return kind, sshalgorithms.NegotiationFailed(kind, offerer, list, strings.Split(m[2], ",")), true
}

// EffectiveLists is one job's evaluation of OpenSSH's own lists: ssh -G
// runs at most once per binary, for the job's first negotiation failure on
// a list OpenSSH chose, and every later one takes its lists, which are the
// same for every device of the job (karvi writes no such list under
// ssh-algorithms.source "transport"). A nil one runs ssh -G each time.
type EffectiveLists struct {
	mu       sync.Mutex
	byBinary map[string]sshalgorithms.Lists
}

// get is binary's lists, from evaluate the first time.
func (e *EffectiveLists) get(binary string, evaluate func() sshalgorithms.Lists) sshalgorithms.Lists {
	if e == nil {
		return evaluate()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if lists, ok := e.byBinary[binary]; ok {
		return lists
	}
	if e.byBinary == nil {
		e.byBinary = map[string]sshalgorithms.Lists{}
	}
	lists := evaluate()
	e.byBinary[binary] = lists
	return lists
}

// effectiveQueries are ssh -G's names for the algorithm lists.
var effectiveQueries = map[string]sshalgorithms.Kind{
	"hostkeyalgorithms": sshalgorithms.HostKey,
	"kexalgorithms":     sshalgorithms.Kex,
	"ciphers":           sshalgorithms.Ciphers,
	"macs":              sshalgorithms.MACs,
}

// effectiveAlgorithms is what OpenSSH's own evaluation of args (the
// session's configuration and device) gives each list: ssh -G, the lists it
// prints; none when it fails.
func effectiveAlgorithms(cfg configload.Snapshot, binary string, args []string) sshalgorithms.Lists {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, append([]string{"-G"}, args...)...)
	cmd.Env = childEnvironment(cfg)
	out, err := cmd.Output()
	lists := sshalgorithms.Lists{}
	if err != nil {
		return lists
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		key, value, _ := strings.Cut(scanner.Text(), " ")
		if kind, ok := effectiveQueries[key]; ok {
			lists[kind] = strings.Split(value, ",")
		}
	}
	return lists
}
