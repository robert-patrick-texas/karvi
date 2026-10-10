package configload

import (
	"github.com/robert-patrick-texas/karvi/internal/matching"
	"github.com/robert-patrick-texas/karvi/internal/sshalgorithms"
)

// SSHAlgorithms is the global algorithm lists of [ssh-algorithms]; a
// snapshot that holds none (not loaded from the registry) has the defaults.
func (s Snapshot) SSHAlgorithms() sshalgorithms.Lists {
	out := sshalgorithms.Defaults()
	for _, k := range sshalgorithms.Kinds {
		if list := s.Strings("ssh-algorithms." + string(k)); len(list) > 0 {
			out[k] = list
		}
	}
	return out
}

// SelectSSHAlgorithms is a device's lists and their source: the profile of
// the [[ssh-algorithms-map]] rule that selects it, else the global lists,
// under the profile's source or ssh-algorithms.source.
func (s Snapshot) SelectSSHAlgorithms(f matching.Fields) (sshalgorithms.Selection, error) {
	return sshalgorithms.Select(s.SSHAlgorithms(), s.String("ssh-algorithms.source"), s.NamedTables("ssh-algorithms-profile"), s.IndexedTables("ssh-algorithms-map"), f)
}
