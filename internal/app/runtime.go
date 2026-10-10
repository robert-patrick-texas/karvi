package app

import (
	"path/filepath"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

type DaemonRuntime struct {
	Config    configload.Snapshot
	Operator  credentials.Operator
	BaseDir   string
	Socket    string
	StatePath string
	LogPath   string
	MaxFrame  int64
	MaxJobs   int
}

// ResolveDaemonRuntime computes private daemon paths and creates the state tree.
// Callers use this only for daemon lifecycle actions, never for help/version.
// Every returned error carries a registered code.
func ResolveDaemonRuntime(common CommonOptions) (DaemonRuntime, error) {
	cfg, operator, err := prepareConfig(common)
	if err != nil {
		return DaemonRuntime{}, err
	}
	base, err := osutil.ResolveBaseDir(cfg.String("basedir"), operator.Home, operator.Username)
	if err != nil {
		return DaemonRuntime{}, errorcodes.Ensure(err, "base_directory_unavailable")
	}
	// The sockets' directory before the state tree, so that one too long
	// for a socket is refused with nothing made.
	sockets, err := osutil.DaemonSockets(cfg.String("daemon.sockets"), base, operator.Home)
	if err != nil {
		return DaemonRuntime{}, err
	}
	socket := filepath.Join(sockets, osutil.DaemonSocketName)
	if err := osutil.EnsureStateTree(base, operator.UID, osutil.DirectoryMode(cfg.String("output.directory-mode"))); err != nil {
		return DaemonRuntime{}, errorcodes.Ensure(err, "state_tree_create_failed")
	}
	return DaemonRuntime{Config: cfg, Operator: operator, BaseDir: base, Socket: socket, StatePath: filepath.Join(base, "state", "daemon.json"), LogPath: filepath.Join(base, "logs", "daemon.log"), MaxFrame: cfg.Int64("daemon.max-ipc-frame-bytes"), MaxJobs: cfg.Int("daemon.max-accepted-jobs")}, nil
}
