package app

import (
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/inventory"
)

// Shortcut is the device word of `karvi DEVICE` (EXAMPLES chapter 71), which
// the parser hands login or command; nothing records it. Word is the first
// word as given, "" when the line began with a command; Near is the command
// word one slip from it, or "".
type Shortcut struct {
	Word string
	Near string
}

// direct reports whether d is the direct target the shortcut's word made: the
// word matched no inventory row.
func (s Shortcut) direct(d inventory.Device) bool {
	return s.Word != "" && d.Source.Name == "direct" && d.ID == "name:"+strings.ToLower(strings.TrimSpace(s.Word))
}

// CheckShortcut is rule 4's guard, where the target set is assembled and
// before any claim: a word one slip from a command word that is no inventory
// name is refused, so a mistyped command never becomes a connection. The
// recording wrapper calls it before its claim, login and command after
// their assembly.
func CheckShortcut(set TargetSet, s Shortcut) error {
	if s.Near == "" {
		return nil
	}
	for _, d := range set.Devices {
		if s.direct(d) {
			return errorcodes.Errorf("cli_command_near_miss", "%q is not a command; did you mean %s?", s.Word, s.Near)
		}
	}
	return nil
}

// refusal is rule 5, where the mode looks its device up: DNS saying the
// shortcut's word does not exist makes it neither a command nor a target. A
// timeout or a server failure keeps its code, the word perhaps a device, as
// does any failure of another device's.
func (s Shortcut) refusal(d inventory.Device, err error) error {
	if !s.direct(d) || errorcodes.Of(err) != "dns_nxdomain" {
		return err
	}
	return errorcodes.Errorf("cli_command_unknown", "%q is neither a command nor a target: DNS has no such name", s.Word)
}
