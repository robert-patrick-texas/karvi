// Package envindirect is the credential CSV's explicit environment
// reference: a result cell env:NAME names a variable whose value the field
// takes. The cell is read with its surrounding white space trimmed and the
// prefix's case ignored; the name is a portable variable name, checked when
// the file loads; the variable is read when a device selects the row,
// through the resolver's one lookup, and one that is unset or empty is
// credential_env_missing, which names it. A cell without the prefix is a
// literal. The env, Redis, and Vault backends' env-indirection flags are
// their own copies of an older rule, for the credential helper to fold.
package envindirect

import (
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/envname"
)

const prefix = "env:"

// Reference reports whether cell is a reference and the name it gives: ref
// is false for a cell without the prefix, a literal; valid is false for a
// reference whose name is not a portable variable name.
func Reference(cell string) (name string, ref, valid bool) {
	c := strings.TrimSpace(cell)
	if len(c) < len(prefix) || !strings.EqualFold(c[:len(prefix)], prefix) {
		return "", false, false
	}
	name = c[len(prefix):]
	return name, true, envname.Portable(name)
}

// Read gives the value of the variable name through lookup, or, when it
// gives none, the reason, which names the variable: "NAME is not set" or
// "NAME is empty". A nil lookup reads no variable.
func Read(name string, lookup func(string) (string, bool)) (value, missing string) {
	v, ok := "", false
	if lookup != nil {
		v, ok = lookup(name)
	}
	switch {
	case !ok:
		return "", name + " is not set"
	case v == "":
		return "", name + " is empty"
	}
	return v, ""
}
