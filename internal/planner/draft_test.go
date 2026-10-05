package planner

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/transportselect"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/platform"
)

var operator = credentials.Operator{Username: "netops", UID: 1000, PrimaryGID: 1000, Groups: []string{"netops"}, Home: fixtureHome}

// k03Set is the smoke inventory as the loader assembles it: explicit system
// transport, default order, the source's provenance.
func k03Set(t *testing.T) TargetSet {
	t.Helper()
	devices := k03(t)
	for i := range devices {
		devices[i].TransportExplicit = true
	}
	return TargetSet{Devices: devices, Order: executionplan.OrderDefault, Provenance: inventory.Provenance{Sources: []inventory.SourceRef{{Name: "smoke", Path: "/tmp/inv.csv", Digest: strings.Repeat("ab", 32)}}}}
}

func draftOptions(commands []string) DraftOptions {
	return DraftOptions{ActivityType: "run", Commands: commands, Follow: true, Address: AddressOptions{Capabilities: &dual}, PlanID: plantest.PlanID, CPUs: 4}
}

// goldenK03Draft pins the draft built from the k03 set with fixed
// identifiers, time, and CPU count. Re-pinned when the five ping and
// display keys entered the builtin layer, which moved every configuration digest and so
// plan.Sources.ConfigDigest; the draft's own fields did not change. Re-pinned
// at plan 3, which adds blind_returns and blind_wait_ns, and again when
// blind and expectations entered without a bump. Re-pinned when the three
// platform-resolution keys entered the builtin layer (registry 11, no bump), which moved every configuration
// digest again; the draft's own fields did not change. Re-pinned when
// display.run.footer entered the builtin layer (registry 11, no bump):
// the configuration digest moved, the draft's own fields did not. Re-pinned
// once for the eight output.files keys (registry 11, no bump), the same
// way: a comparison of the two drafts showed config_digest as the only
// line that differs. Re-pinned once for daemon.shutdown-idle-timer
// (registry 11, no bump), the same way and with the same comparison. The registry
// moved to 12 at the v0.13.0 number for those ten keys at once (11 had
// been released in v0.12.0); the number is not in the digest, so the pin
// did not move with it. Re-pinned once at the configuration review:
// twenty keys nothing read and the session-cap pattern left the builtin
// layer, which moved every configuration digest; registry 12 then, and 13
// at the v0.14.0 number, which moved no digest: the number is not in it.
// Re-pinned in the same review for the spool threshold's removal (digest
// moves are no concern at this stage).
// Re-pinned once when five display defaults gained the "! " prefix, and the configuration digest covers the
// builtin layer's values, so it moved with them; the draft's own fields did
// not (config_digest the only line that differs).
// Re-pinned at plan schema 4: the draft's output
// gained persist, files, and root, and its sources lost the inventory
// digest, so the draft's own fields changed for the first time since 3.
// Re-pinned at plan schema 5: the schema
// number in the draft moved; the draft's own fields did not.
// Re-pinned at plan schema 7: the schema number
// moved for platform_commands and the collection sub-block, neither in
// this draft.
// Re-pinned at plan schema 6: the draft's output gained crop_to_dot, and
// the configuration digest moved with registry 14's four keys and two
// changed defaults.
// Re-pinned at registry 15: the configuration
// digest moved with watch.refresh's default; the draft is the same.
// Re-pinned at registry 16: the configuration digest moved with the
// sharedroot key; the draft's own fields did not change. Re-pinned at
// registry 17: the digest moved with
// platform-resolution.default's shipped value, cisco_iosxe; the k03 rows
// set their platforms, so the draft's targets did not change. Re-pinned at
// registry 18: the digest moved with crun.after and crun.after-timeout;
// the draft's own fields did not change. Re-pinned at registry 19 and plan
// schema 9: the schema number is in the digest; the draft's own fields did
// not change. Re-pinned at registry 20: the configuration digest moved
// with spooldir, spoolfreecheck, and output.spool-threshold-bytes entering
// the builtin layer; the plan schema stays 9 and the draft's own fields did
// not change. Re-pinned at registry 21:
// freecheck replaced spoolfreecheck and output.min-free-bytes-after-job's
// default moved to 2 GiB; the plan schema stays 9.
// Re-pinned at the rename to karvi:
// the draft's output root names the executable's XDG directory and the
// configuration digest covers texts that name the executable, so both
// moved with the name; no counter moved and the draft's own fields did
// not change. Re-pinned once more when the fixture home was fixed: the fixture's
// home is the tests' own fixed path, not the real operator's, so the
// draft's output root is /tmp/karvi-k03-home/.local/share/karvi/jobs on
// every host; the draft's own fields did not change.
// Re-pinned at registry 22: display.ping.header,
// a template, replaced the boolean display.ping; the plan schema stays 9.
// Re-pinned at registry 23: display.record.header and
// display.record.footer were added; the plan schema stays 9.
// Re-pinned at registry 24 and plan schema 10: display.collection.footer
// was added and the collection's word entered the plan.
// Re-pinned when output.files.failures-jsonl became errors-jsonl, and the
// plan's failures_jsonl errors_jsonl; registry 24 and plan schema 10 stay.
// Re-pinned at registry 25 and plan schema 11: each target carries its
// channel.
const goldenK03Draft = "fc30504506844c7cc2fc455f04e81fdfda0b64372126205d3bde6ca67b130a32"

func TestDraftFromK03PinsDigest(t *testing.T) {
	cfg := testConfig(t)
	draft, err := Draft(context.Background(), cfg, operator, k03Set(t), draftOptions(plantest.Commands), plantest.DraftedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := draft.Validate(executionplan.Draft); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(draft, "", "  ")
	t.Logf("draft:\n%s", data)
	sum, err := executionplan.SumPlan(draft)
	if err != nil {
		t.Fatal(err)
	}
	if sum.String() != goldenK03Draft {
		t.Errorf("draft digest %s, golden %s", sum, goldenK03Draft)
	}
	// The digest depends on the configuration digest, so the pin holds only
	// while the internal-only configuration is stable; show it.
	t.Logf("config digest %s", cfg.Digest)
	if draft.PlanID != plantest.PlanID || len(draft.Targets) != 2 || draft.Targets[0].TargetID != "name:core-a" || draft.Targets[1].TargetID != "name:edge-b" || draft.Dispatch.Width != 4 || draft.Dispatch.StartWidth != 16 || draft.Dispatch.MaxWidth != 32 || !draft.Output.Follow || draft.Ping.Enabled || draft.Ping.Probes != 2 || draft.Ping.TimeoutNS != int64(500*time.Millisecond) || draft.Operator.Username != "netops" {
		t.Fatalf("draft=%+v", draft)
	}
	for _, x := range draft.Targets {
		if x.Device.Transport != "system" || x.Device.Port != 22 || x.AddressPlan.Selected.String() != "127.0.0.1" {
			t.Fatalf("target=%+v", x)
		}
	}
}

// TestDraftSettingsPrecedence: the dispatch settings are the configuration's
// keys (run's Dispatch options reach them as overrides in the cli layer),
// configuration over the CPU default.
func TestDraftSettingsPrecedence(t *testing.T) {
	sets := []string{"dispatch.default=wave", "dispatch.parallel-workers=8", "dispatch.wave-start-width=20", "dispatch.wave-max-width=40", "dispatch.halt-on-error-count=5", "dispatch.halt-on-error-percent=50", "dispatch.wave-gate-error-count=6", "dispatch.wave-gate-error-percent=60", `dispatch.wave-gate-timed-delay="7s"`, "display.run.echo=true"}
	set := k03Set(t)
	for _, tc := range []struct {
		name string
		sets []string
		opts func(*DraftOptions)
		want executionplan.DispatchSettings
		echo bool
	}{
		{"cpu defaults", nil, nil, executionplan.DispatchSettings{Mode: "serial", Width: 4, StartWidth: 16, MaxWidth: 32, DispatchOrder: "default"}, false},
		{"configuration beats cpu", sets, nil, executionplan.DispatchSettings{Mode: "wave", Width: 8, StartWidth: 20, MaxWidth: 40, DispatchOrder: "default", HaltErrorCount: 5, HaltErrorPercent: 50, WaveGateErrorCount: 6, WaveGateErrorPercent: 60, WaveGateTimedDelayNS: int64(7 * time.Second)}, true},
		{"continue-device-on-error from the run", sets, func(o *DraftOptions) {
			o.ContinueDeviceOnError = true
		}, executionplan.DispatchSettings{Mode: "wave", Width: 8, StartWidth: 20, MaxWidth: 40, DispatchOrder: "default", HaltErrorCount: 5, HaltErrorPercent: 50, WaveGateErrorCount: 6, WaveGateErrorPercent: 60, WaveGateTimedDelayNS: int64(7 * time.Second), ContinueDeviceOnError: true}, true},
	} {
		opts := draftOptions(plantest.Commands)
		if tc.opts != nil {
			tc.opts(&opts)
		}
		draft, err := Draft(context.Background(), testConfig(t, tc.sets...), operator, set, opts, plantest.DraftedAt)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if draft.Dispatch != tc.want || draft.Output.Echo != tc.echo {
			t.Errorf("%s: dispatch=%+v echo=%v, want %+v echo=%v", tc.name, draft.Dispatch, draft.Output.Echo, tc.want, tc.echo)
		}
	}
	// command mode is serial, width 1, whatever the configuration says; the
	// shuffle key travels with the order; jsonl is a plan format.
	key := "rollout-7"
	shuffled := set
	shuffled.Order, shuffled.ShuffleKey = executionplan.OrderShuffle, &key
	opts := draftOptions(plantest.Commands[:1])
	opts.ActivityType, opts.Format = "command", "jsonl"
	draft, err := Draft(context.Background(), testConfig(t, sets...), operator, shuffled, opts, plantest.DraftedAt)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Dispatch.Mode != "serial" || draft.Dispatch.Width != 1 || draft.Dispatch.DispatchOrder != "shuffle" || *draft.Dispatch.ShuffleKey != key || draft.Output.Format != "jsonl" || draft.Output.Echo {
		t.Fatalf("command draft=%+v", draft)
	}
}

// TestDraftFixesTransportKindAndPort: the projection's transport is the
// selection kind and its port the effective port, non-zero for a device
// without one under a platform override; Telnet is gated.
func TestDraftFixesTransportKindAndPort(t *testing.T) {
	set := k03Set(t)
	draft, err := Draft(context.Background(), testConfig(t, "platform.generic.ssh-port=2222"), operator, set, draftOptions(plantest.Commands), plantest.DraftedAt)
	if err != nil {
		t.Fatal(err)
	}
	if x := draft.Targets[0]; x.Device.Transport != "system" || x.Device.Port != 2222 {
		t.Fatalf("target=%+v", x.Device)
	}
	telnet := k03Set(t)
	telnet.Devices[0].Transport = "telnet"
	if _, err := Draft(context.Background(), testConfig(t), operator, telnet, draftOptions(plantest.Commands), plantest.DraftedAt); errorcodes.Of(err) != "telnet_not_allowed" {
		t.Fatalf("telnet gate: %v", err)
	}
	draft, err = Draft(context.Background(), testConfig(t, "security.allow-telnet=true"), operator, telnet, draftOptions(plantest.Commands), plantest.DraftedAt)
	if err != nil {
		t.Fatal(err)
	}
	if x := draft.Targets[0]; x.Device.Transport != "telnet" || x.Device.Port != 23 {
		t.Fatalf("telnet target=%+v", x.Device)
	}
	if _, err := Draft(context.Background(), testConfig(t), operator, set, draftOptions(nil), plantest.DraftedAt); errorcodes.Of(err) != "activity_scope_empty" {
		t.Fatalf("no commands: %v", err)
	}
}

// TestHeaderAtDraftAndCommitted: the same key and job ID,
// the draft digest before preparation and the final digest after.
func TestHeaderAtDraftAndCommitted(t *testing.T) {
	draft, err := Draft(context.Background(), testConfig(t), operator, k03Set(t), draftOptions(plantest.Commands), plantest.DraftedAt)
	if err != nil {
		t.Fatal(err)
	}
	h, err := Header(draft, plantest.JobID, executionplan.ModeLive)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Validate(executionplan.Draft); err != nil {
		t.Fatal(err)
	}
	if err := h.Matches(&draft, executionplan.Draft); err != nil {
		t.Fatal(err)
	}
	if h.IdempotencyKey != plantest.JobID || h.JobID != plantest.JobID || h.Client.AppName != "karvi" || h.Client.PID == 0 || h.Priority != 0 || h.ExecutionDomain != "local" {
		t.Fatalf("header=%+v", h)
	}
	for i := range draft.Targets {
		draft.Targets[i].CredentialBindingID = plantest.GrantA
	}
	final, err := executionplan.Finalize(draft, plantest.FinalizedAt)
	if err != nil {
		t.Fatal(err)
	}
	c, err := CommitHeader(final, plantest.JobID, executionplan.ModeLive, executionplan.PackageReference{Protection: executionplan.ProtectionLocalPeer, Digest: executionplan.Sum([]byte("package"))})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(executionplan.Committed); err != nil {
		t.Fatal(err)
	}
	if err := c.Matches(&final, executionplan.Committed); err != nil {
		t.Fatal(err)
	}
	if c.PlanDigest != final.PlanDigest || c.PlanDigest == h.PlanDigest {
		t.Fatalf("commit header digest %s, final %s, draft %s", c.PlanDigest, final.PlanDigest, h.PlanDigest)
	}
}

// The interactive-prompt declarations reach the plan as given, a command
// with no declaration gets an empty list rather than null, and a pattern
// that does not compile is execution_plan_invalid at drafting, before any
// daemon call.
func TestDraftCarriesTheDeclarations(t *testing.T) {
	cfg := testConfig(t)
	opts := draftOptions([]string{"copy running-config startup-config", "reload"})
	opts.Blind = []bool{false, true}
	opts.Expectations = [][]executionplan.Expectation{{{Pattern: `filename \[startup-config\]\?`, Response: ""}}, nil}
	draft, err := Draft(context.Background(), cfg, operator, k03Set(t), opts, plantest.DraftedAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Blind) != 2 || draft.Blind[0] || !draft.Blind[1] {
		t.Errorf("blind %v, want [false true]", draft.Blind)
	}
	if len(draft.Expectations) != 2 || len(draft.Expectations[0]) != 1 || draft.Expectations[0][0].Pattern != `filename \[startup-config\]\?` || draft.Expectations[1] == nil || len(draft.Expectations[1]) != 0 {
		t.Errorf("expectations %v", draft.Expectations)
	}
	data, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if want := `"expectations":[[{"pattern":"filename \\[startup-config\\]\\?","response":""}],[]]`; !strings.Contains(string(data), want) {
		t.Errorf("plan JSON lacks %s:\n%s", want, data)
	}
	// The planner's copy is the plan's own: editing the options afterwards
	// does not reach it.
	opts.Expectations[0][0].Pattern = "("
	if draft.Expectations[0][0].Pattern == "(" {
		t.Error("the plan shares the caller's declaration list")
	}
	if _, err := Draft(context.Background(), cfg, operator, k03Set(t), opts, plantest.DraftedAt); err == nil || !strings.HasPrefix(err.Error(), "execution_plan_invalid: expectations: command 1 declaration 1: pattern \"(\"") {
		t.Errorf("uncompilable pattern: %v", err)
	}
}

// TestCheckFileNames covers the file-name collision rule: two devices whose
// file names are one are refused naming both; without the crop the same
// pair is two files; an address and a name never collide; a collection
// names its suffixed file.
func TestCheckFileNames(t *testing.T) {
	target := func(name string) executionplan.ExecutionTarget {
		return executionplan.ExecutionTarget{Device: executionplan.DeviceProjection{CanonicalName: name}}
	}
	pair := []executionplan.ExecutionTarget{target("core.example.net"), target("r1"), target("core.example.com")}
	err := checkFileNames(pair, true, nil)
	if errorcodes.Of(err) != "output_file_name_collision" || !strings.Contains(err.Error(), "core.example.net and core.example.com") || !strings.Contains(err.Error(), "output.core.txt") {
		t.Fatalf("the pair under the crop: %v", err)
	}
	if err := checkFileNames(pair, false, nil); err != nil {
		t.Fatalf("the pair without the crop: %v", err)
	}
	if err := checkFileNames([]executionplan.ExecutionTarget{target("10.1.2.3"), target("10-1-2-3.example.net")}, true, nil); errorcodes.Of(err) != "output_file_name_collision" {
		t.Fatalf("an address and a name that crops to its spelling are one file: %v", err)
	}
	if err := checkFileNames([]executionplan.ExecutionTarget{target("Core.example.net"), target("core.example.net")}, false, nil); errorcodes.Of(err) != "output_file_name_collision" {
		t.Fatalf("two spellings of one name are one file: %v", err)
	}
	// A collection names its own file, the suffix appended.
	err = checkFileNames(pair, true, &executionplan.CollectionSettings{Suffix: ".cfg"})
	if errorcodes.Of(err) != "output_file_name_collision" || !strings.Contains(err.Error(), "the file core.cfg;") {
		t.Fatalf("a collection's file: %v", err)
	}
}

// TestDraftPlatformCommands covers the platform command lists and the
// collection sub-block in the planner: with PlatformCommands and no command the
// plan carries each target platform's list and the digest covers it; a
// platform without a list refuses the draft naming it; with a command
// the lists are not consulted; Collection fills the output's sub-block
// from crun.directory (auto is <basedir>/crun), crun.file-mode, and the
// word, and a crun's alone turns output.NAME.txt off.
func TestDraftPlatformCommands(t *testing.T) {
	operator := credentials.Operator{Username: "netops", UID: 1000, PrimaryGID: 1000, Home: t.TempDir()}
	opts := draftOptions(nil)
	opts.PlatformCommands, opts.Collection = true, "crun"
	cfg := testConfig(t, `platform.generic.crun-commands=["show version", "show clock"]`, "crun.file-mode=\"0644\"")
	draft, err := Draft(context.Background(), cfg, operator, k03Set(t), opts, plantest.DraftedAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Commands) != 0 || len(draft.PlatformCommands) != 1 || len(draft.PlatformCommands["generic"]) != 2 {
		t.Fatalf("lists: commands %q platform_commands %q", draft.Commands, draft.PlatformCommands)
	}
	if draft.CommandPlanDigest != executionplan.SumCommandPlan(nil, draft.PlatformCommands) {
		t.Fatal("the digest does not cover the lists")
	}
	if err := draft.Validate(executionplan.Draft); err != nil {
		t.Fatal(err)
	}
	c := draft.Output.Collection
	if c == nil || !strings.HasSuffix(c.Directory, "/crun") || c.FileMode != "0644" || c.Word != "crun" {
		t.Fatalf("collection: %+v", c)
	}
	if draft.Output.Files.OutputTxt {
		t.Fatal("a crun's plan writes output.NAME.txt")
	}
	if !strings.HasPrefix(c.Directory, "/") {
		t.Fatalf("the directory is not absolute: %s", c.Directory)
	}
	// generic has no built-in list: refused naming the platform and a device.
	_, err = Draft(context.Background(), testConfig(t), operator, k03Set(t), opts, plantest.DraftedAt)
	if errorcodes.Of(err) != "crun_platform_commands_missing" || !strings.Contains(err.Error(), "platform generic") || !strings.Contains(err.Error(), "name:core-a") {
		t.Fatalf("a platform without a list: %v", err)
	}
	// With a command the lists are not consulted.
	with := draftOptions(plantest.Commands[:1])
	with.PlatformCommands = true
	draft, err = Draft(context.Background(), testConfig(t), operator, k03Set(t), with, plantest.DraftedAt)
	if err != nil || len(draft.PlatformCommands) != 0 || len(draft.Commands) != 1 || draft.Output.Collection != nil {
		t.Fatalf("with a command: %v %q %q %+v", err, draft.Commands, draft.PlatformCommands, draft.Output.Collection)
	}
	// An explicit directory, relative to the working directory.
	cfg = testConfig(t, `platform.generic.crun-commands=["show version"]`, "crun.directory=\"configs\"")
	draft, err = Draft(context.Background(), cfg, operator, k03Set(t), opts, plantest.DraftedAt)
	if err != nil || !filepath.IsAbs(draft.Output.Collection.Directory) || filepath.Base(draft.Output.Collection.Directory) != "configs" || draft.Output.Collection.FileMode != "0660" {
		t.Fatalf("an explicit directory: %v %+v", err, draft.Output.Collection)
	}
	// A run's or a command's collection (--cd): the word carried, the
	// folder's output.NAME.txt kept as it is without one.
	for _, word := range []string{"run", "command"} {
		o := draftOptions(plantest.Commands[:1])
		o.ActivityType, o.Collection = word, word
		draft, err = Draft(context.Background(), cfg, operator, k03Set(t), o, plantest.DraftedAt)
		if err != nil || draft.Output.Collection == nil || draft.Output.Collection.Word != word || !draft.Output.Files.OutputTxt {
			t.Fatalf("%s's collection: %v %+v %+v", word, err, draft.Output.Collection, draft.Output.Files)
		}
		if err := draft.Validate(executionplan.Draft); err != nil {
			t.Fatalf("%s's collection: %v", word, err)
		}
	}
}

// A target's channel is its platform's, resolved at planning: shell carried
// in the plan; exec over telnet refused naming both; exec on an SSH
// transport refused until the exec channel is built.
func TestTargetChannel(t *testing.T) {
	ssh := transportselect.Selection{Kind: transportselect.KindNative, Implementation: "scrapligo-v1"}
	telnet := transportselect.Selection{Kind: transportselect.KindTelnet, Implementation: "telnet"}
	tables := map[string]map[string]any{"srv": {"driver": "linux", "channel": "exec"}}
	if c, err := targetChannel(platform.Resolve("linux_shell", tables), "r1", ssh); err != nil || c != platform.ChannelShell {
		t.Fatalf("linux_shell: %q %v", c, err)
	}
	if _, err := targetChannel(platform.Resolve("srv", tables), "srv1", telnet); errorcodes.Of(err) != "channel_exec_over_telnet" || !strings.Contains(err.Error(), "srv1: platform srv") || !strings.Contains(err.Error(), "telnet") {
		t.Fatalf("exec over telnet: %v", err)
	}
	if _, err := targetChannel(platform.Resolve("srv", tables), "srv1", ssh); errorcodes.Of(err) != "channel_exec_unavailable" || !strings.Contains(err.Error(), "scrapligo-v1") {
		t.Fatalf("exec: %v", err)
	}
	draft, err := Draft(context.Background(), testConfig(t), operator, k03Set(t), draftOptions(plantest.Commands), plantest.DraftedAt)
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range draft.Targets {
		if tg.Channel != executionplan.ChannelShell {
			t.Fatalf("%s: channel %q", tg.TargetID, tg.Channel)
		}
	}
}
