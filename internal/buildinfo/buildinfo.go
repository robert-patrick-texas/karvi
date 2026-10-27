// Package buildinfo owns the single version/build identity used by all karvi
// executables. Release automation sets the variables with -ldflags.
package buildinfo

import "runtime"

const (
	AppName              = "karvi"
	Version              = "0.36.0"
	ConfigSchemaVersion  = 6
	CommandRecordSchema  = 4
	ScoreboardSchema     = 5
	AuditSchema          = 2
	JobSchema            = 5
	DaemonIPCSchema      = 12
	ConfigRegistrySchema = 32
)

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

func Current() Info {
	return Info{
		Application: AppName, Version: Version, Commit: Commit, Build: BuildOrNone(),
		GoVersion: runtime.Version(), ConfigSchemaVersion: ConfigSchemaVersion,
		CommandRecordSchemaVersion: CommandRecordSchema, ScoreboardSchemaVersion: ScoreboardSchema,
		AuditSchemaVersion: AuditSchema, JobSchemaVersion: JobSchema,
		DaemonIPCSchemaVersion: DaemonIPCSchema, ConfigRegistrySchemaVersion: ConfigRegistrySchema,
		FIPSMode: FIPSMode, SSHTransports: sshTransports(),
	}
}
