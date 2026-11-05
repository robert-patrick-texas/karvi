// Package buildinfo owns the single version/build identity used by all karvi
// executables. Release automation sets the variables with -ldflags.
package buildinfo

import (
	"runtime"

	"github.com/robert-patrick-texas/karvi"
	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/records"
)

const AppName = "karvi"

// Version is the release's number, read from VERSION at the tree's root, the
// one place it is written.
var Version = karvi.Version

var (
	Commit   = "development"
	FIPSMode = "false"
)

// SSHTransport is one SSH transport this executable carries: the native
// transport compiled in, and OpenSSH run as an external executable.
type SSHTransport struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Linkage string `json:"linkage"`
}

// sshTransports are the two, by ID.
func sshTransports() []SSHTransport {
	return []SSHTransport{
		{ID: "native", Name: "native", Linkage: "compiled-in"},
		{ID: "system", Name: "system", Linkage: "external-executable"},
	}
}

// Info is the stable version projection rendered by `karvi version`.
type Info struct {
	Application                 string `json:"application"`
	Version                     string `json:"version"`
	Commit                      string `json:"commit"`
	Build                       string `json:"build"`
	GoVersion                   string `json:"go_version"`
	ConfigSchemaVersion         int    `json:"config_schema_version"`
	CommandRecordSchemaVersion  int    `json:"command_record_schema_version"`
	ScoreboardSchemaVersion     int    `json:"scoreboard_schema_version"`
	AuditSchemaVersion          int    `json:"audit_schema_version"`
	JobSchemaVersion            int    `json:"job_schema_version"`
	DaemonIPCSchemaVersion      int    `json:"daemon_ipc_schema_version"`
	ConfigRegistrySchemaVersion int    `json:"config_registry_schema_version"`
	FIPSMode                    string `json:"fips_mode"`
	// ICMPMethod is the ICMP gate method this process detected; the version
	// command fills it, Current leaves it empty.
	ICMPMethod    string         `json:"icmp_method,omitempty"`
	SSHTransports []SSHTransport `json:"ssh_transports"`
}

// Current is this executable's projection. The schema counters it reports
// are their owners' constants: a counter is written once, in the package that
// owns its schema.
func Current() Info {
	return Info{
		Application:                 AppName,
		Version:                     Version,
		Commit:                      Commit,
		Build:                       BuildOrNone(),
		GoVersion:                   runtime.Version(),
		ConfigSchemaVersion:         configschema.ConfigSchemaVersion,
		CommandRecordSchemaVersion:  records.CommandSchemaVersion,
		ScoreboardSchemaVersion:     records.ScoreboardSchemaVersion,
		AuditSchemaVersion:          records.AuditSchemaVersion,
		JobSchemaVersion:            records.JobSchemaVersion,
		DaemonIPCSchemaVersion:      ipc.SchemaVersion,
		ConfigRegistrySchemaVersion: configschema.RegistrySchemaVersion,
		FIPSMode:                    FIPSMode,
		SSHTransports:               sshTransports(),
	}
}
