package planner

import (
	"context"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/inventory"
)

func row(name, platformName string, line int) inventory.Device {
	d := inventory.Direct(name, platformName, "system", 0)
	d.Source = inventory.SourceRef{Name: "lab", Path: "/inv.csv", Line: line}
	return d
}

// TestResolvePlatform covers platform resolution for one device: the
// platform used, the notice, and the
// refusal, for a row and a direct target, under the default configuration,
// a configured default, and on-unknown = "warn" with and without a fallback.
func TestResolvePlatform(t *testing.T) {
	base := testConfig(t)
	withDefault := testConfig(t, `platform-resolution.default="cisco_iosxe"`)
	cleared := testConfig(t, `platform-resolution.default=""`) // the site's way to generic
	warn := testConfig(t, `platform-resolution.on-unknown="warn"`)
	warnFallback := testConfig(t, `platform-resolution.on-unknown="warn"`, `platform-resolution.unknown-fallback="cisco_iosxe"`)
	failFallback := testConfig(t, `platform-resolution.unknown-fallback="cisco_iosxe"`)
	for _, tc := range []struct {
		name   string
		cfg    configload.Snapshot
		device inventory.Device
		used   string
		notice string // the code, or "" for none, or "platform_unknown" for the refusal
	}{
		{"blank row", base, row("r1", "", 7), "cisco_iosxe", NoticePlatformNotSet}, // the shipped default since registry 17
		{"blank row with the default cleared", cleared, row("r1", "", 7), "generic", NoticePlatformNotSet},
		{"blank row with the default", withDefault, row("r1", "", 7), "cisco_iosxe", NoticePlatformNotSet},
		{"direct target", base, inventory.Direct("r1", "", "system", 0), "cisco_iosxe", ""},
		{"direct target with the default cleared", cleared, inventory.Direct("r1", "", "system", 0), "generic", ""},
		{"direct target with the default", withDefault, inventory.Direct("r1", "", "system", 0), "cisco_iosxe", ""},
		{"direct target with --platform beside the default", withDefault, inventory.Direct("r1", "generic", "system", 0), "generic", ""},
		{"known", base, row("r1", "cisco_iosxe", 7), "cisco_iosxe", ""},
		{"known in another spelling", base, row("r1", " Cisco_IOSXE ", 7), "cisco_iosxe", ""},
		{"unknown under fail", base, row("r1", "cisco_iosx", 7), "", "platform_unknown"},
		{"unknown under fail with a fallback", failFallback, row("r1", "cisco_iosx", 7), "", "platform_unknown"},
		{"unknown under warn", warn, row("r1", "cisco_iosx", 7), "generic", NoticePlatformUnknownFallback},
		{"unknown under warn with a fallback", warnFallback, row("r1", "cisco_iosx", 7), "cisco_iosxe", NoticePlatformUnknownFallback},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolvePlatform(tc.cfg, tc.device)
			if tc.notice == "platform_unknown" {
				if errorcodes.Of(err) != "platform_unknown" {
					t.Fatalf("err=%v, want platform_unknown", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Platform != tc.used {
				t.Fatalf("used %q, want %q", got.Platform, tc.used)
			}
			if tc.notice == "" {
				if got.Notice != nil {
					t.Fatalf("unexpected notice %+v", got.Notice)
				}
				return
			}
			if got.Notice == nil || got.Notice.Code != tc.notice {
				t.Fatalf("notice %+v, want %s", got.Notice, tc.notice)
			}
			want := map[string]string{"supplied": tc.device.Platform, "source": "lab line 7", "used": tc.used}
			for k, v := range want {
				if got.Notice.Details[k] != v {
					t.Fatalf("detail %s=%q, want %q (%+v)", k, got.Notice.Details[k], v, got.Notice.Details)
				}
			}
		})
	}
}

// TestResolvePlatformsGrouping is the refusal and warning message shape:
// one clause or warning line per distinct unknown value per source,
// in inventory order, with the source and first line, the count, and the
// first three devices; one line per source for not-set rows; the known
// platforms once in the refusal; and no warning when the set is refused.
func TestResolvePlatformsGrouping(t *testing.T) {
	devices := []inventory.Device{
		row("core1", "cisco_iosx", 2), row("core2", "cisco_iosx", 3), row("core3", "cisco_iosx", 4), row("core4", "cisco_iosx", 5),
		row("edge1", "junos", 6), row("lab1", "", 7), row("lab2", "", 8), row("sw-ios", "cisco_iosxe", 9),
	}
	other := row("far1", "cisco_iosx", 2)
	other.Source.Name = "remote"
	devices = append(devices, other)
	var warnings []string
	warn := func(s string) { warnings = append(warnings, s) }
	_, err := ResolvePlatforms(testConfig(t, `platform-resolution.default="cisco_iosxe"`), devices, warn)
	if errorcodes.Of(err) != "platform_unknown" {
		t.Fatalf("err=%v", err)
	}
	want := `platform_unknown: inventory source lab line 2: platform "cisco_iosx" is not a known platform; 4 devices: core1, core2, core3, …; inventory source lab line 6: platform "junos" is not a known platform; 1 device: edge1; inventory source remote line 2: platform "cisco_iosx" is not a known platform; 1 device: far1 (known: generic, cisco_iosxe, cisco_iosxr, cisco_nxos, juniper_junos, arista_eos, linux)`
	if err.Error() != want {
		t.Fatalf("message\n got %s\nwant %s", err.Error(), want)
	}
	if len(warnings) != 0 {
		t.Fatalf("a refusal printed warnings: %q", warnings)
	}
	out, err := ResolvePlatforms(testConfig(t, `platform-resolution.default="cisco_iosxe"`, `platform-resolution.on-unknown="warn"`), devices, warn)
	if err != nil {
		t.Fatal(err)
	}
	wantLines := []string{
		`platform_unknown_fallback: inventory source lab line 2: platform "cisco_iosx" is not a known platform; 4 devices proceed as generic (platform-resolution.unknown-fallback): core1, core2, core3, …`,
		`platform_unknown_fallback: inventory source lab line 6: platform "junos" is not a known platform; 1 device proceeds as generic (platform-resolution.unknown-fallback): edge1`,
		`platform_unknown_fallback: inventory source remote line 2: platform "cisco_iosx" is not a known platform; 1 device proceeds as generic (platform-resolution.unknown-fallback): far1`,
		`platform_not_set: inventory source lab: 2 devices have no platform; they proceed as cisco_iosxe (platform-resolution.default): lab1, lab2`,
	}
	if strings.Join(warnings, "\n") != strings.Join(wantLines, "\n") {
		t.Fatalf("warnings\n got %s\nwant %s", strings.Join(warnings, "\n"), strings.Join(wantLines, "\n"))
	}
	used := []string{}
	for _, r := range out {
		code := "-"
		if r.Notice != nil {
			code = r.Notice.Code
		}
		used = append(used, r.Platform+"/"+code)
	}
	if got := strings.Join(used, " "); got != "generic/platform_unknown_fallback generic/platform_unknown_fallback generic/platform_unknown_fallback generic/platform_unknown_fallback generic/platform_unknown_fallback cisco_iosxe/platform_not_set cisco_iosxe/platform_not_set cisco_iosxe/- generic/platform_unknown_fallback" {
		t.Fatalf("resolutions %s", got)
	}
	warnings = nil
	if _, err := ResolvePlatforms(testConfig(t, `platform-resolution.default=""`), []inventory.Device{row("lab1", "", 7)}, warn); err != nil || len(warnings) != 1 || warnings[0] != `platform_not_set: inventory source lab: 1 device has no platform; it proceeds as generic (platform-resolution.default is empty): lab1` {
		t.Fatalf("cleared default: err=%v warnings=%q", err, warnings)
	}
}

// TestSetPlatform: the platform a device is
// matched on is blank for not set and, under on-unknown = "warn", for an
// unknown name; otherwise the normalised set name; "fail" keeps an unknown
// name set so a selector reaches it.
func TestSetPlatform(t *testing.T) {
	fail := testConfig(t, `platform.C9300.driver="cisco_iosxe"`)
	warn := testConfig(t, `platform.C9300.driver="cisco_iosxe"`, `platform-resolution.on-unknown="warn"`)
	for _, tc := range []struct{ set, underFail, underWarn string }{
		{"", "", ""},
		{"cisco_iosxe", "cisco_iosxe", "cisco_iosxe"},
		{" Cisco_IOSXE ", "cisco_iosxe", "cisco_iosxe"},
		{"c9300", "c9300", "c9300"},
		{"cisco_iosx", "cisco_iosx", ""},
	} {
		d := row("r1", tc.set, 2)
		if got := SetPlatform(fail, d); got != tc.underFail {
			t.Errorf("%q under fail: %q, want %q", tc.set, got, tc.underFail)
		}
		if got := SetPlatform(warn, d); got != tc.underWarn {
			t.Errorf("%q under warn: %q, want %q", tc.set, got, tc.underWarn)
		}
	}
}

// TestDraftCarriesPlatformNotices: the plan target of a not-set row
// carries the platform_not_set
// notice with its details, a set row carries none, and the plan validates.
func TestDraftCarriesPlatformNotices(t *testing.T) {
	cfg := testConfig(t, `platform-resolution.default="cisco_iosxe"`)
	set := k03Set(t)
	set.Devices[0].Platform = ""
	draft, err := Draft(context.Background(), cfg, operator, set, draftOptions(plantest.Commands), plantest.DraftedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := draft.Validate(executionplan.Draft); err != nil {
		t.Fatal(err)
	}
	a, b := draft.Targets[0], draft.Targets[1]
	if a.Device.Platform != "cisco_iosxe" || len(a.Notices) != 1 || a.Notices[0].Code != "platform_not_set" || a.Notices[0].Details["source"] != "smoke line 2" || a.Notices[0].Details["used"] != "cisco_iosxe" {
		t.Fatalf("not-set target: platform=%s notices=%+v", a.Device.Platform, a.Notices)
	}
	if b.Device.Platform != "generic" || b.Notices != nil {
		t.Fatalf("set target: platform=%s notices=%+v", b.Device.Platform, b.Notices)
	}
}
