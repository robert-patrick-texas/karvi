// Package app coordinates karvi use cases without depending on command-line
// parsing. Requests contain no credentials so the same types may cross the
// private per-UID daemon boundary.
package app

import (
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
	"github.com/robert-patrick-texas/karvi/records"
)

// IO owns the three conventional process streams used by an invocation.
type IO = jobexec.IO

// CommonOptions are shared by direct and daemon-backed work.
type CommonOptions struct {
	ConfigRoots     []string       `json:"config_roots,omitempty"`
	Sets            []string       `json:"sets,omitempty"`
	ConfigFlags     map[string]any `json:"config_flags,omitempty"`
	Quiet           bool           `json:"quiet"`
	Debug           bool           `json:"debug"`
	DebugShowSecret bool           `json:"debug_show_secrets"`
}

// CommandOptions describes scripted work on the first device of a target
// set.
type CommandOptions struct {
	CommonOptions
	Targets               []records.TargetInput         `json:"targets"`
	Excludes              []string                      `json:"excludes,omitempty"`
	Address               string                        `json:"address,omitempty"`
	Platform              string                        `json:"platform,omitempty"`
	Transport             string                        `json:"transport,omitempty"`
	Port                  int                           `json:"port,omitempty"`
	AddressAuthority      string                        `json:"address_authority,omitempty"` // client | daemon for the first device
	Commands              []string                      `json:"commands"`
	CommandsFile          string                        `json:"commands_file,omitempty"` // the --cf path; its base name reaches the scoreboard
	BlindReturns          []int                         `json:"blind_returns,omitempty"` // empty, or one count per command
	Blind                 []bool                        `json:"blind,omitempty"`         // empty, or one tolerance flag per command
	Expectations          [][]executionplan.Expectation `json:"expectations,omitempty"`  // empty, or one expect-and-send list per command
	Format                string                        `json:"format"`
	Echo                  bool                          `json:"echo"`
	DynamicBorder         bool                          `json:"dynamic_border"`
	NoBorder              bool                          `json:"no_border"`
	ContinueDeviceOnError bool                          `json:"continue_device_on_error"`
	Collection            string                        `json:"collection,omitempty"` // command given --cd: the plan carries the collection
	Suffix                string                        `json:"suffix,omitempty"`     // --fs: the collection file names' suffix
}

// RunOptions is safe to submit to the same-UID daemon. It deliberately carries
// selectors and configuration roots, never a resolved credential.
type RunOptions struct {
	CommonOptions
	ActivityID            string                        `json:"activity_id,omitempty"`
	Targets               []records.TargetInput         `json:"targets"`
	Excludes              []string                      `json:"excludes,omitempty"`
	ManagementAddress     string                        `json:"management_address,omitempty"`
	Platform              string                        `json:"platform,omitempty"`            // --platform: every device of the set runs as this known platform
	AddressAuthorities    []string                      `json:"address_authorities,omitempty"` // TARGET=client|daemon, in order
	Commands              []string                      `json:"commands"`
	CommandsFile          string                        `json:"commands_file,omitempty"` // the --cf path; its base name reaches the scoreboard
	BlindReturns          []int                         `json:"blind_returns,omitempty"` // empty, or one count per command
	Blind                 []bool                        `json:"blind,omitempty"`         // empty, or one tolerance flag per command
	Expectations          [][]executionplan.Expectation `json:"expectations,omitempty"`  // empty, or one expect-and-send list per command
	Dispatch              string                        `json:"dispatch,omitempty"`
	Workers               int                           `json:"workers,omitempty"`
	StartWidth            int                           `json:"start_width,omitempty"`
	MaxWidth              int                           `json:"max_width,omitempty"`
	HaltErrorCount        int                           `json:"halt_error_count,omitempty"`
	HaltErrorPercent      int                           `json:"halt_error_percent,omitempty"`
	WaveGateErrorCount    int                           `json:"wave_gate_error_count,omitempty"`
	WaveGateErrorPercent  int                           `json:"wave_gate_error_percent,omitempty"`
	WaveDelay             time.Duration                 `json:"wave_delay,omitempty"`
	ContinueDeviceOnError bool                          `json:"continue_device_on_error"`
	PlatformCommands      bool                          `json:"platform_commands,omitempty"` // crun with no command: each device its platform's crun-commands (12.9)
	Collection            string                        `json:"collection,omitempty"`        // crun, or run given --cd: the word; the plan carries the collection directory, file mode, and word
	Suffix                string                        `json:"suffix,omitempty"`            // --fs: the collection file names' suffix
	Transport             string                        `json:"transport,omitempty"`
	Format                string                        `json:"format"`
	Echo                  bool                          `json:"echo"`
	DynamicBorder         bool                          `json:"dynamic_border"`
	NoBorder              bool                          `json:"no_border"`
	Detach                bool                          `json:"detach"`
	// Follow renders the job's records as they become durable; false
	// follows the stream for its terminal only.
	Follow bool `json:"follow"`
	// Exercise submits in exercise mode: the daemon
	// validates everything but the device and the client renders
	// exercise.json instead of records.
	Exercise bool `json:"exercise"`
}

// LoginOptions describes an interactive session with the first device of a
// target set.
type LoginOptions struct {
	CommonOptions
	Targets   []records.TargetInput `json:"targets"`
	Excludes  []string              `json:"excludes,omitempty"`
	Address   string                `json:"address,omitempty"`
	Platform  string                `json:"platform,omitempty"`
	Transport string                `json:"transport,omitempty"`
	Port      int                   `json:"port,omitempty"`
	// AddressAuthority is client or daemon for the first device.
	AddressAuthority string `json:"address_authority,omitempty"`
}

// ActivityResult is the safe terminal result returned locally or over IPC.
type ActivityResult = jobexec.ActivityResult
