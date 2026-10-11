package envname

import "testing"

// TestPortable: a letter or an underscore, then letters, digits, and
// underscores; anything else, the empty name included, is not one.
func TestPortable(t *testing.T) {
	for name, want := range map[string]bool{
		"NETPASS": true, "NETPASS_NYC": true, "_x9": true, "a": true, "VAULT_TOKEN": true,
		"": false, "9X": false, "NETPASS-NYC": false, " X": false, "p$ss": false, "VAULT TOKEN": false, "j.smith": false, "Ä": false,
	} {
		if got := Portable(name); got != want {
			t.Errorf("%q: %v, want %v", name, got, want)
		}
	}
}
