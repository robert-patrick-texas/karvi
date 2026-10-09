// Package native is the composition boundary for built-in Go SSH adapters.
// Core execution depends only on platform.Driver; each optional implementation
// registers an opener from its provider file's init (the scrapligo_v1 build
// tag that once gated the provider is gone: every executable carries it).
package native

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/devsession"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/sshalgorithms"
	"github.com/robert-patrick-texas/karvi/platform"
)

// Factory selects one compiled-in implementation by stable identifier.
type Factory struct {
	Implementation string
	Config         configload.Snapshot
	Home           string
	// BaseDir is the operator's private root, which holds the trust store
	// under ssh.known-hosts-file "auto".
	BaseDir        string
	MaxOutputBytes int64
	// Spool is the session's spool: the directory,
	// the threshold, and the activity; the device is filled per session.
	Spool devsession.Spool
	Debug func(string)
	// Algorithms are the device's SSH algorithm lists; empty means the
	// configuration's global lists.
	Algorithms sshalgorithms.Lists
}

type opener func(context.Context, Factory, platform.OpenRequest) (platform.Driver, error)

// implementation is one registered opener and the base platforms it admits
// (a table per implementation).
type implementation struct {
	open   opener
	admits []string
}

var registry = struct {
	sync.RWMutex
	openers map[string]implementation
}{openers: map[string]implementation{}}

func register(id string, open opener, admits ...string) {
	registry.Lock()
	defer registry.Unlock()
	if id == "" || open == nil || len(admits) == 0 {
		panic("native transport: incomplete registration")
	}
	if _, exists := registry.openers[id]; exists {
		panic("native transport: duplicate registration: " + id)
	}
	registry.openers[id] = implementation{open: open, admits: admits}
}

// Admits refuses a platform whose base driver the implementation's table
// does not admit (native_platform_not_qualified); the label is never read.
// An implementation not compiled in is the open's
// error, not this one.
func Admits(id string, def platform.Definition) error {
	registry.RLock()
	impl, ok := registry.openers[id]
	registry.RUnlock()
	if !ok {
		return nil
	}
	for _, base := range impl.admits {
		if base == def.Base {
			return nil
		}
	}
	return errorcodes.Errorf("native_platform_not_qualified", "%s does not admit platform %q (base driver %s); it admits %s", id, def.Name, def.Base, strings.Join(impl.admits, ", "))
}

// Available reports whether this exact executable contains the requested
// native implementation.
func Available(id string) bool {
	registry.RLock()
	defer registry.RUnlock()
	_, ok := registry.openers[id]
	return ok
}

// Implementations returns stable identifiers for every compiled-in native SSH
// adapter. The result is sorted for deterministic diagnostics.
func Implementations() []string {
	registry.RLock()
	defer registry.RUnlock()
	out := make([]string, 0, len(registry.openers))
	for id := range registry.openers {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (f Factory) Open(ctx context.Context, req platform.OpenRequest) (platform.Driver, error) {
	registry.RLock()
	impl, ok := registry.openers[f.Implementation]
	registry.RUnlock()
	if !ok {
		return nil, fmt.Errorf("native_transport_unavailable: implementation %q is not compiled into this karvi executable", f.Implementation)
	}
	return impl.open(ctx, f, req)
}
