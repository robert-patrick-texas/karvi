// Package app coordinates karvi use cases without depending on command-line
// parsing. The options types are an invocation's, used in its own process:
// the daemon receives the plan made from them, never the types.
package app

import (
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
	"github.com/robert-patrick-texas/karvi/records"
)

// IO owns the three conventional process streams used by an invocation.
type IO = jobexec.IO

// CommonOptions are an invocation's one read of its configuration
// (ReadConfig) and the options every stage takes beside it.
type CommonOptions struct {
	Config          configload.Snapshot
	Operator        credentials.Operator
	Quiet           bool
	Debug           bool
	DebugShowSecret bool
}

// CommandOptions describes scripted work on the first device of a target
// set.
type CommandOptions struct {
	CommonOptions
	Targets          []records.TargetInput
	Excludes         []string
	Address          string
	Platform         string
	Transport        string
	Port             int
	AddressAuthority string // client | daemon for the first device
	Commands         []string
	CommandsFile     string                        // the --cf path; its base name reaches the scoreboard
	BlindReturns     []int                         // empty, or one count per command
	Blind            []bool                        // empty, or one tolerance flag per command
	Expectations     [][]executionplan.Expectation // empty, or one expect-and-send list per command
	Timeouts         []int64                       // empty, or one --timeout per command in nanoseconds, 0 the job's
	MaxBytes         []int64                       // empty, or one --maxbytes per command, 0 the job's
	Format           string
	Echo             bool
	DynamicBorder    bool
	NoBorder         bool
	Collection       string // command given --cd: the plan carries the collection
	Suffix           string // --fs: the collection file names' suffix
}

// RunOptions describes a run: its target inputs, its commands and their
// declarations, and how it is shown; a daemon-backed run sends the plan made
// from it.
type RunOptions struct {
	CommonOptions
	ActivityID         string
	Targets            []records.TargetInput
	Excludes           []string
	ManagementAddress  string
	Platform           string   // --platform: every device of the set runs as this known platform
	AddressAuthorities []string // TARGET=client|daemon, in order
	Commands           []string
	CommandsFile       string                        // the --cf path; its base name reaches the scoreboard
	BlindReturns       []int                         // empty, or one count per command
	Blind              []bool                        // empty, or one tolerance flag per command
	Expectations       [][]executionplan.Expectation // empty, or one expect-and-send list per command
	Timeouts           []int64                       // empty, or one --timeout per command in nanoseconds, 0 the job's
	MaxBytes           []int64                       // empty, or one --maxbytes per command, 0 the job's
	PlatformCommands   bool                          // crun with no command: each device its platform's crun-commands (12.9)
	Collection         string                        // crun, or run given --cd: the word; the plan carries the collection directory and word
	Suffix             string                        // --fs: the collection file names' suffix
	Transport          string
	Format             string
	Echo               bool
	DynamicBorder      bool
	NoBorder           bool
	Detach             bool
	// Follow renders the job's records as they become durable; false
	// follows the stream for its terminal only.
	Follow bool
	// Exercise submits in exercise mode: the daemon
	// validates everything but the device and the client renders
	// exercise.json instead of records.
	Exercise bool
}

// LoginOptions describes an interactive session with the first device of a
// target set.
type LoginOptions struct {
	CommonOptions
	Targets   []records.TargetInput
	Excludes  []string
	Address   string
	Platform  string
	Transport string
	Port      int
	// AddressAuthority is client or daemon for the first device.
	AddressAuthority string
}

// ActivityResult is the safe terminal result returned locally or over IPC.
type ActivityResult = jobexec.ActivityResult
