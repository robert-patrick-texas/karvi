package cli

import (
	"path/filepath"

	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/internal/capacity"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/hostkey"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/scoreboard"
)

// placeResolver names, for `config show --explain`, the place each key
// whose value is a place karvi writes or reads comes to for the next
// activity in process, by the activity's own chooser and without creating
// anything: the private root, the trust store, the three trees, the
// scratch, the spool, the control sockets, the scoreboards, the ledger, the
// daemon's socket, and, when set, the audit file, the inventory sources'
// files, and the credential backends' files. The configuration is the one
// the invocation loaded; a running daemon is never asked.
func placeResolver(snap configload.Snapshot) configload.Resolver {
	var (
		loaded         bool
		op             operatorPlaces
		opErr, baseErr error
		base           string
	)
	operator := func() {
		if loaded {
			return
		}
		loaded = true
		o, err := osutil.CurrentOperator()
		if err != nil {
			opErr = err
			return
		}
		op = operatorPlaces{home: o.Home, username: o.Username, uid: o.UID}
		base, baseErr = osutil.BaseDirPath(snap.String("basedir"), o.Home, o.Username)
	}
	return func(key string) (configload.Place, bool) {
		raw := snap.String(key)
		resolve, ok := placeKeys[key]
		if key == "audit.file" && raw == "" {
			ok = false // a file key has a place only when set
		} else if !ok {
			resolve, ok = dynamicPlace(key, raw)
		}
		if !ok {
			return configload.Place{}, false
		}
		operator()
		if opErr != nil {
			return configload.Place{Err: opErr}, true
		}
		if baseErr != nil && (key == "basedir" || followsBase(key, raw)) {
			return configload.Place{Err: baseErr}, true
		}
		pl, err := resolve(snap, raw, op, base)
		return fromPlace(pl, err), true
	}
}

// operatorPlaces is what a place's rule needs of the operator.
type operatorPlaces struct {
	home, username string
	uid            int
}

// placeRule resolves one place key's value, raw, as its activity would.
type placeRule func(snap configload.Snapshot, raw string, op operatorPlaces, base string) (osutil.Place, error)

// placeKeys are the fixed keys whose value is a place, each by its rule.
var placeKeys = map[string]placeRule{
	"basedir": func(_ configload.Snapshot, _ string, _ operatorPlaces, base string) (osutil.Place, error) {
		return osutil.Place{Path: base}, nil
	},
	"ssh.known-hosts-file": func(_ configload.Snapshot, raw string, op operatorPlaces, base string) (osutil.Place, error) {
		p, err := hostkey.StorePath(raw, op.home, base)
		return osutil.Place{Path: p}, err
	},
	"output.root":     treeRule(osutil.OutputRootPlace),
	"transcript.root": treeRule(osutil.TranscriptRootPlace),
	"crun.directory":  treeRule(osutil.CrunDirectoryPlace),
	"tempdir": func(_ configload.Snapshot, raw string, op operatorPlaces, base string) (osutil.Place, error) {
		return osutil.ScratchPlace(raw, base, op.home, op.username, op.uid)
	},
	"spooldir": func(_ configload.Snapshot, raw string, op operatorPlaces, _ string) (osutil.Place, error) {
		return osutil.SpoolPlace(raw, op.home, op.uid)
	},
	"ssh.control-path-root": func(_ configload.Snapshot, raw string, op operatorPlaces, base string) (osutil.Place, error) {
		return osutil.ControlPathRootPlace(raw, base, op.home, op.username, op.uid)
	},
	"scoreboards": func(_ configload.Snapshot, raw string, op operatorPlaces, base string) (osutil.Place, error) {
		return scoreboard.Place(raw, op.home, base)
	},
	"sessions.shared-capacity-root": func(_ configload.Snapshot, raw string, op operatorPlaces, base string) (osutil.Place, error) {
		return capacity.Place(raw, op.home, base)
	},
	"daemon.socket": func(_ configload.Snapshot, raw string, op operatorPlaces, base string) (osutil.Place, error) {
		p, err := osutil.DaemonSocket(raw, base, op.home)
		return osutil.Place{Path: p}, err
	},
	"audit.file": func(_ configload.Snapshot, raw string, op operatorPlaces, _ string) (osutil.Place, error) {
		p, err := osutil.ResolvePath(raw, op.home)
		if err == nil {
			err = osutil.CheckSetupPlaces(filepath.Dir(p), "audit.file")
		}
		return osutil.Place{Path: p}, err
	},
}

// treeRule is the rule of one of the three trees.
func treeRule(place func(raw, shared, base, home string) (string, error)) placeRule {
	return func(snap configload.Snapshot, raw string, op operatorPlaces, base string) (osutil.Place, error) {
		p, err := place(raw, snap.String("sharedroot"), base, op.home)
		return osutil.Place{Path: p}, err
	}
}

// followsBase says whether key's place under raw comes from the private
// root, so a root that cannot be found is its refusal too: every place key
// at auto but the spool, and the trust store, which also follows it.
func followsBase(key, raw string) bool {
	switch key {
	case "spooldir", "audit.file":
		return false
	case "ssh.known-hosts-file":
		return hostkey.StoreUsesBase(raw)
	}
	_, fixed := placeKeys[key]
	return fixed && (raw == "" || raw == "auto")
}

// dynamicPlace is the rule of a dynamic table's file key, when set: a key
// the registry marks as a path (configschema.Place), an inventory source's
// path or a credential backend's path and TLS files, which the load made
// absolute, each by osutil.ResolvePath.
func dynamicPlace(key, raw string) (placeRule, bool) {
	if _, fixed := configschema.Lookup(key); fixed || raw == "" {
		return nil, false
	}
	if _, ok := configschema.Place(key); !ok {
		return nil, false
	}
	return func(_ configload.Snapshot, raw string, op operatorPlaces, _ string) (osutil.Place, error) {
		p, err := osutil.ResolvePath(raw, op.home)
		return osutil.Place{Path: p}, err
	}, true
}

// fromPlace is osutil's place as the view prints it.
func fromPlace(pl osutil.Place, err error) configload.Place {
	out := configload.Place{Path: pl.Path, Err: err}
	for _, p := range pl.Passed {
		out.Passed = append(out.Passed, configload.PassedCandidate{Path: p.Path, Reason: p.Reason})
	}
	return out
}
