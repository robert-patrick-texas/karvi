// Package buildinfo owns the single version/build identity used by all karvi
// executables. Release automation sets the variables with -ldflags.
package buildinfo

import (
	"runtime"
	"sort"
	"sync"
)

const (
	AppName              = "karvi"
	Version              = "0.25.0"
	ConfigSchemaVersion  = 6
	CommandRecordSchema  = 2
	ScoreboardSchema     = 3
	AuditSchema          = 1
	JobSchema            = 2
	DaemonIPCSchema      = 10
	ConfigRegistrySchema = 23
)

var (
	Commit    = "development"
	BuildTime = "unknown"
	FIPSMode  = "false"
)

// SSHTransport describes an SSH implementation that is usable by this exact
// executable. Entries are registered by build-composition files, so version
// output never claims that an adapter is present merely because configuration
// names it as a preference.
type SSHTransport struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Linkage string `json:"linkage"`
}

var transportRegistry = struct {
	sync.RWMutex
	items map[string]SSHTransport
}{items: map[string]SSHTransport{}}

func init() {
	registerSSHTransport(SSHTransport{ID: "system", Name: "system", Linkage: "external-executable"})
}

func registerSSHTransport(t SSHTransport) {
	transportRegistry.Lock()
	defer transportRegistry.Unlock()
	if t.ID == "" || t.Name == "" || t.Linkage == "" {
		panic("buildinfo: incomplete SSH transport registration")
	}
	if _, exists := transportRegistry.items[t.ID]; exists {
		panic("buildinfo: duplicate SSH transport registration: " + t.ID)
	}
	transportRegistry.items[t.ID] = t
}

func sshTransports() []SSHTransport {
	transportRegistry.RLock()
	defer transportRegistry.RUnlock()
	out := make([]SSHTransport, 0, len(transportRegistry.items))
	for _, item := range transportRegistry.items {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Info is the stable version projection rendered by `karvi version`.
type Info struct {
	Application                 string `json:"application"`
	Version                     string `json:"version"`
	Commit                      string `json:"commit"`
	BuildTime                   string `json:"build_time"`
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
		Application: AppName, Version: Version, Commit: Commit, BuildTime: BuildTime,
		GoVersion: runtime.Version(), ConfigSchemaVersion: ConfigSchemaVersion,
		CommandRecordSchemaVersion: CommandRecordSchema, ScoreboardSchemaVersion: ScoreboardSchema,
		AuditSchemaVersion: AuditSchema, JobSchemaVersion: JobSchema,
		DaemonIPCSchemaVersion: DaemonIPCSchema, ConfigRegistrySchemaVersion: ConfigRegistrySchema,
		FIPSMode: FIPSMode, SSHTransports: sshTransports(),
	}
}
