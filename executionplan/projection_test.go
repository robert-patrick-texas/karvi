package executionplan

import (
	"reflect"
	"testing"

	"github.com/robert-patrick-texas/karvi/inventory"
)

// excludedDeviceFields are inventory.Device fields that the projection drops
// on purpose: the schema version belongs to the plan, the two address fields
// live in AddressPlan, and the two transient fields are not inventory facts
// (InputToken becomes ExecutionTarget.input_target). The address authority
// is an input to the address plan and lives there.
// The credential pin is an input to client-side credential resolution and
// the daemon has no use for it; the key a pinned device took is in the
// grant's match evidence.
var excludedDeviceFields = map[string]string{
	"SchemaVersion":     "plan schema version governs",
	"ManagementAddress": "AddressPlan",
	"Addresses":         "AddressPlan",
	"AddressAuthority":  "AddressPlan.authority",
	"TransportExplicit": "transient",
	"PlatformUsed":      "transient: the planner's device view for the enable rule; the projection's platform is the platform used",
	"InputToken":        "ExecutionTarget.input_target",
	"CredKeyRef":        "an input to client-side credential resolution: no reader of the plan uses it, the key taken is the grant's matched_on.credkey, and projecting it later is additive",
}

func TestProjectionParityWithInventoryDevice(t *testing.T) {
	dev := reflect.TypeOf(inventory.Device{})
	proj := reflect.TypeOf(DeviceProjection{})
	for i := 0; i < dev.NumField(); i++ {
		f := dev.Field(i)
		if _, excluded := excludedDeviceFields[f.Name]; excluded {
			if _, present := proj.FieldByName(f.Name); present {
				t.Errorf("%s is excluded yet projected", f.Name)
			}
			continue
		}
		pf, ok := proj.FieldByName(f.Name)
		if !ok {
			t.Errorf("inventory.Device.%s is neither projected nor excluded", f.Name)
			continue
		}
		if pf.Type != f.Type {
			t.Errorf("%s: projection type %s, device type %s", f.Name, pf.Type, f.Type)
		}
		if pf.Tag.Get("json") != f.Tag.Get("json") {
			t.Errorf("%s: projection tag %q, device tag %q", f.Name, pf.Tag.Get("json"), f.Tag.Get("json"))
		}
	}
	for name := range excludedDeviceFields {
		if _, ok := dev.FieldByName(name); !ok {
			t.Errorf("exclusion list names %s, which inventory.Device no longer has", name)
		}
	}
}

func TestProjectDeviceCopiesAndNormalizes(t *testing.T) {
	cap := 4
	d := inventory.Device{ID: "name:x", Name: "X", CanonicalName: "x", Platform: "generic", Transport: "native", SessionCap: &cap, Attributes: map[string]string{"rack": "r1"}, Groups: []string{"g"}}
	p := ProjectDevice(d)
	*d.SessionCap = 9
	d.Attributes["rack"] = "r2"
	d.Groups[0] = "h"
	if *p.SessionCap != 4 || p.Attributes["rack"] != "r1" || p.Groups[0] != "g" {
		t.Fatal("projection aliases the device")
	}
	empty := ProjectDevice(inventory.Device{ID: "name:y", Name: "y"})
	if empty.Groups == nil || empty.Attributes == nil || empty.SessionCap != nil {
		t.Fatalf("empty projection must have empty, non-nil collections: %+v", empty)
	}
}
