package sshalgorithms

import (
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/matching"
)

func TestDefaultsStrongestFirst(t *testing.T) {
	d := Defaults()
	want := Lists{
		HostKey: {"ssh-ed25519", "ecdsa-sha2-nistp521", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp256", "rsa-sha2-512", "rsa-sha2-256", "ssh-rsa"},
		Kex: {"mlkem768x25519-sha256", "sntrup761x25519-sha512@openssh.com", "curve25519-sha256", "curve25519-sha256@libssh.org",
			"ecdh-sha2-nistp521", "ecdh-sha2-nistp384", "ecdh-sha2-nistp256", "diffie-hellman-group18-sha512", "diffie-hellman-group16-sha512",
			"diffie-hellman-group-exchange-sha256", "diffie-hellman-group14-sha256", "diffie-hellman-group14-sha1"},
		Ciphers: {"aes256-gcm@openssh.com", "chacha20-poly1305@openssh.com", "aes256-ctr", "aes256-cbc"},
		MACs:    {"hmac-sha2-512-etm@openssh.com", "hmac-sha2-256-etm@openssh.com", "hmac-sha2-512", "hmac-sha2-256", "hmac-sha1"},
	}
	if !reflect.DeepEqual(d, want) {
		t.Fatalf("defaults %v", d)
	}
	d[Ciphers][0] = "changed"
	if Defaults()[Ciphers][0] != "aes256-gcm@openssh.com" {
		t.Fatal("Defaults shares its slices")
	}
	for _, k := range Kinds {
		if code, err := CheckList(k, Defaults()[k], Global); err != nil {
			t.Fatalf("%s defaults: %s %v", k, code, err)
		}
	}
}

func TestCheckList(t *testing.T) {
	cases := []struct {
		kind  Kind
		names []string
		scope Scope
		code  string
	}{
		{Ciphers, nil, Global, "config_ssh_algorithm_list_empty"},
		{Ciphers, []string{"aes256-gcm@openssh.com", "aes256-gcm@openssh.com"}, Global, "config_ssh_algorithm_duplicate"},
		{Ciphers, []string{"aes256-gcm"}, Global, "config_ssh_algorithm_unknown"},
		{Kex, []string{"aes256-ctr"}, Global, "config_ssh_algorithm_unknown"},
		{Ciphers, []string{"3des-cbc"}, Profile, "config_ssh_algorithm_forbidden"},
		{Ciphers, []string{"blowfish-cbc"}, Profile, "config_ssh_algorithm_forbidden"},
		{Ciphers, []string{"arcfour256"}, Profile, "config_ssh_algorithm_forbidden"},
		{Ciphers, []string{"none"}, Profile, "config_ssh_algorithm_forbidden"},
		{HostKey, []string{"ssh-dss"}, Profile, "config_ssh_algorithm_forbidden"},
		{HostKey, []string{"ssh-ed25519-cert-v01@openssh.com"}, Profile, "config_ssh_algorithm_forbidden"},
		{HostKey, []string{"sk-ssh-ed25519@openssh.com"}, Profile, "config_ssh_algorithm_forbidden"},
		{MACs, []string{"hmac-md5"}, Profile, "config_ssh_algorithm_forbidden"},
		{MACs, []string{"hmac-sha1-96"}, Profile, "config_ssh_algorithm_forbidden"},
		{MACs, []string{"umac-64-etm@openssh.com"}, Profile, "config_ssh_algorithm_forbidden"},
		{Ciphers, []string{"aes128-ctr"}, Global, "config_ssh_algorithm_profile_only"},
		{Kex, []string{"diffie-hellman-group1-sha1"}, Global, "config_ssh_algorithm_profile_only"},
		{MACs, []string{"umac-128@openssh.com"}, Global, "config_ssh_algorithm_profile_only"},
		{Ciphers, []string{"aes128-cbc", "aes192-ctr"}, Profile, ""},
		{Kex, []string{"diffie-hellman-group-exchange-sha1"}, Profile, ""},
		{Ciphers, []string{"aes256-ctr"}, Global, ""},
	}
	for _, c := range cases {
		code, err := CheckList(c.kind, c.names, c.scope)
		if code != c.code || (c.code == "") != (err == nil) {
			t.Errorf("%s %q scope %d: code %q err %v, want %q", c.kind, c.names, c.scope, code, err, c.code)
		}
	}
}

func TestCheckProfileAndApply(t *testing.T) {
	global := Defaults()
	ok := map[string]any{"kex-append": []any{"diffie-hellman-group1-sha1"}, "ciphers": []any{"aes128-ctr", "aes128-cbc"}}
	if err := CheckProfile(ok, global); err != nil {
		t.Fatal(err)
	}
	got := Apply(global, ok)
	if kex := got[Kex]; kex[len(kex)-1] != "diffie-hellman-group1-sha1" || len(kex) != len(global[Kex])+1 {
		t.Fatalf("kex %q", kex)
	}
	if !reflect.DeepEqual(got[Ciphers], []string{"aes128-ctr", "aes128-cbc"}) || !reflect.DeepEqual(got[MACs], global[MACs]) {
		t.Fatalf("applied %v", got)
	}
	if !reflect.DeepEqual(global, Defaults()) {
		t.Fatal("Apply changed the global lists")
	}
	for _, c := range []struct {
		profile map[string]any
		code    string
	}{
		{map[string]any{}, "config_ssh_algorithms_profile_empty"},
		{map[string]any{"kex": []any{"curve25519-sha256"}, "kex-append": []any{"diffie-hellman-group1-sha1"}}, "config_ssh_algorithms_profile_list_conflict"},
		{map[string]any{"ciphers-append": []any{"aes256-ctr"}}, "config_ssh_algorithm_duplicate"},
		{map[string]any{"ciphers-append": []any{"3des-cbc"}}, "config_ssh_algorithm_forbidden"},
		{map[string]any{"macs": []any{}}, "config_ssh_algorithm_list_empty"},
	} {
		err := CheckProfile(c.profile, global)
		pe, isProfile := err.(*ProfileError)
		if !isProfile || pe.Code != c.code {
			t.Errorf("%v: %v, want %s", c.profile, err, c.code)
		}
	}
}

func TestSelect(t *testing.T) {
	global := Defaults()
	profiles := map[string]map[string]any{
		"old":  {"ciphers-append": []any{"aes128-ctr"}},
		"lab":  {"kex-append": []any{"diffie-hellman-group1-sha1"}},
		"lab2": {"macs-append": []any{"umac-128@openssh.com"}},
	}
	rules := []map[string]any{
		{"profile": "old", "device-group": "legacy"},
		{"profile": "lab", "address-cidr": "192.0.2.0/24"},
		{"profile": "lab2", "address-cidr": "192.0.2.0/25"},
	}
	f := matching.Fields{Name: "r1", Address: netip.MustParseAddr("198.51.100.1"), Groups: []string{"core"}}
	s, err := Select(global, profiles, rules, f)
	if err != nil || s.Profile != "" || s.Rule != -1 || !reflect.DeepEqual(s.Lists, global) {
		t.Fatalf("unmatched: %+v %v", s, err)
	}
	f.Groups = []string{"legacy"}
	if s, err = Select(global, profiles, rules, f); err != nil || s.Profile != "old" || s.Rule != 0 || s.Lists[Ciphers][len(s.Lists[Ciphers])-1] != "aes128-ctr" {
		t.Fatalf("group rule: %+v %v", s, err)
	}
	f.Groups, f.Address = nil, netip.MustParseAddr("192.0.2.10")
	if s, err = Select(global, profiles, rules, f); err != nil || s.Profile != "lab2" {
		t.Fatalf("longest prefix: %+v %v", s, err)
	}
	rules = append(rules, map[string]any{"profile": "lab", "address-cidr": "192.0.2.0/25"})
	if _, err = Select(global, profiles, rules, f); errorcodes.Of(err) != "ssh_algorithms_map_ambiguous" || !strings.Contains(err.Error(), "ssh-algorithms-map.2, ssh-algorithms-map.3") {
		t.Fatalf("ambiguous: %v", err)
	}
}

func TestOffer(t *testing.T) {
	implements := func(k Kind, name string) bool { return !strings.HasPrefix(name, "mlkem") && name != "aes256-cbc" }
	got, err := Defaults().Offer("system", implements)
	if err != nil || got[Kex][0] != "sntrup761x25519-sha512@openssh.com" || contains(got[Ciphers], "aes256-cbc") {
		t.Fatalf("offer %v %v", got, err)
	}
	_, err = Lists{HostKey: {"ssh-ed25519"}, Kex: {"curve25519-sha256"}, Ciphers: {"aes256-cbc"}, MACs: {"hmac-sha1"}}.Offer("scrapligo-v1", implements)
	if errorcodes.Of(err) != "ssh_algorithms_unavailable" || !strings.Contains(err.Error(), "cipher algorithms (aes256-cbc) is implemented by scrapligo-v1") {
		t.Fatalf("unavailable: %v", err)
	}
}

func TestNegotiationFailed(t *testing.T) {
	err := NegotiationFailed(Kex, []string{"curve25519-sha256"}, []string{"diffie-hellman-group1-sha1", "kex-strict-s-v00@openssh.com"})
	if errorcodes.Of(err) != "ssh_algorithm_negotiation_failed" || !strings.HasSuffix(err.Error(), "no key exchange algorithm in common: karvi offered curve25519-sha256; the device offered diffie-hellman-group1-sha1") {
		t.Fatalf("%v", err)
	}
}
