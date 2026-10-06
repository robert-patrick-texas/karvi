package main

// groups are the index's headings and the left column's, in their order.
var groups = []string{
	"Start here",
	"Operating karvi",
	"Reference",
	"Design and records",
	"Building, qualification, and release",
	"The project",
}

// document is one Markdown file of the tree: its path from the tree's root,
// its group, and the line the index gives it (Markdown code spans allowed).
type document struct{ path, group, about string }

// documents is every Markdown file the tree holds outside vendor/, each once;
// the converter refuses a tree with a file this table does not name, or a
// name the tree does not hold. The order is the index's within each group.
var documents = []document{
	{"README.md", "Start here", "What karvi is and does, part by part, with the guide for each."},
	{"docs/QUICKSTART.md", "Start here", "A minimal configuration, then a login, one-device commands, and a fleet run."},
	{"examples/README.md", "Start here", "One commented example of each file karvi reads, in the shape a site keeps it."},

	{"docs/OPERATIONS.md", "Operating karvi", "The operator's guide: upgrades, dispatch, the watch screen, credentials, the job's files, the shared trees, retention."},
	{"docs/COLLECTION.md", "Operating karvi", "`karvi crun`: device output collected into a tree a site commits, its lists, the hook, and the schedule."},
	{"docs/CREDENTIAL-CSV.md", "Operating karvi", "The credential CSV: the file, how a row is chosen, keys and pins, its owner and mode."},
	{"docs/COMMAND-SESSION.md", "Operating karvi", "One device session: the command grammar, prompts, blind sends, echo, and device errors."},
	{"docs/COMMAND-TROUBLESHOOTING.md", "Operating karvi", "What to capture, and how to read it, when a command misbehaves."},
	{"docs/SSH-TROUBLE.md", "Operating karvi", "An SSH session that never reaches the prompt: OpenSSH at `-vvv` under karvi through a wrapper, what karvi and a recorded login show, and the device's side."},
	{"docs/DISPLAY-CONFIGURATION.md", "Operating karvi", "The terminal display: headers and footers, borders, width, colour, and output formats."},
	{"docs/LOGIN-TRANSCRIPTS.md", "Operating karvi", "A recorded login: where its transcript goes, its formats, and what it holds."},
	{"docs/PRUNE.md", "Operating karvi", "`karvi-prune`: what retention removes and when, its report, exits, flags, and schedules."},
	{"docs/SCALE.md", "Operating karvi", "A large network from a many-core host: the width, the free-space check, the memory budget."},
	{"docs/SSH-HOST-KEY-POLICY.md", "Operating karvi", "The host-key policy and the trust store: enrollment, changed keys, diagnostics."},
	{"docs/SSH-TRANSPORTS.md", "Operating karvi", "The two SSH transports, `system` and `scrapligo-v1`, and how one is selected."},
	{"docs/TIMEOUTS.md", "Operating karvi", "What bounds a device session: the keys, how they relate, what each expiry records."},

	{"docs/FILES.md", "Reference", "Every directory and file karvi reads or writes, shared and individual: mode, owner, group, and how each place is chosen."},
	{"docs/ERROR-CODES.md", "Reference", "Every exit status, error code, record reason, and notice, generated from the registry."},

	{"docs/ARCHITECTURE.md", "Design and records", "How the implementation is built: planning, execution, display, transports, the daemon and storage."},
	{"docs/DESIGN.md", "Design and records", "The settled design decisions, and why each was taken."},
	{"docs/TRANSPORT-DRIVER-ARCHITECTURE.md", "Design and records", "The transport and driver layers karvi owns, and the rule for adding one."},
	{"docs/EXAMPLES.md", "Design and records", "Each design session as it was executed against the tree, with the alternatives not taken."},
	{"CHANGELOG.md", "Design and records", "What each release changed, the breaking changes first."},
	{"ROADMAP.md", "Design and records", "What is decided and not built yet, in the order it will be taken up."},

	{"BUILDING.md", "Building, qualification, and release", "The release build, in brief."},
	{"BUILD-HOWTO.md", "Building, qualification, and release", "An Ubuntu host prepared, the source bundle built offline, installed, and rolled back."},
	{"docs/BUILD-QUALIFICATION.md", "Building, qualification, and release", "The gates a build and a release pass."},
	{"docs/CISCO-IOSXE-QUALIFICATION.md", "Building, qualification, and release", "What qualifies `cisco_iosxe` on real devices, and what the fake already shows."},
	{"docs/DEVICE-QUALIFICATION-RUNBOOK.md", "Building, qualification, and release", "The laboratory run, row by row, and the evidence it keeps."},
	{"release/BUILD-RESULT.md", "Building, qualification, and release", "The current release's build and qualification result."},
	{"release/DOWNLOADS.md", "Building, qualification, and release", "The current release's artifacts and their checksums."},
	{"release/SCRAPLIGO-EVIDENCE.md", "Building, qualification, and release", "The vendored scrapligo module's integration evidence."},

	{"CONTRIBUTING.md", "The project", "The principles a change keeps, building and testing, branches and releases."},
	{"SECURITY.md", "The project", "Reporting a vulnerability, and the boundaries: host keys, credentials, legacy cryptography, Telnet."},
	{"LICENSE.md", "The project", "The MIT License karvi is released under."},
}
