// Package envindirect is a credential store's explicit environment
// reference: a credential CSV's result cell, or a Redis or Vault record's
// username, password, or enable field, env:NAME names a variable whose value
// the field takes. The value is read with its surrounding white space
// trimmed and the prefix's case ignored; the name is a portable variable
// name, which the CSV checks when the file loads and Redis and Vault when the
// record is read; the variable is read through the resolver's one lookup,
// and one that is unset or empty is credential_env_missing, which names it.
// A value without the prefix is a literal.
package envindirect

import (
	"fmt"
	"strings"

	"github.com/robert-patrick-texas/karvi/credentials"
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
// gives none, the reason, which names the variable: "NAME not set" or
// "NAME empty". A nil lookup reads no variable.
func Read(name string, lookup func(string) (string, bool)) (value, missing string) {
	v, ok := "", false
	if lookup != nil {
		v, ok = lookup(name)
	}
	switch {
	case !ok:
		return "", name + " not set"
	case v == "":
		return "", name + " empty"
	}
	return v, ""
}

// Missing is a credential source's failure when a variable it names gives
// no value: credential_env_missing, naming the backend, where the variable
// is named (a file, line, and column; a record and field; or a key), and
// Read's reason. The device stops at planning and never moves on to a later
// backend.
func Missing(backend, where, reason string) credentials.BackendResult {
	return credentials.BackendResult{Outcome: credentials.Incomplete, ErrorCode: "credential_env_missing", Message: fmt.Sprintf("credential backend %s: %s: %s", backend, where, reason)}
}

// Record is a Redis or Vault record's username, password, and enable
// fields, named by names in that order and read by get: each a literal as
// written, or the value of the variable a reference names, read through
// lookup; found is whether the record holds any of the three. A reference
// whose variable gives no value is Missing, naming the record and the field;
// one whose rest is not a portable variable name is the store's own code for
// a malformed record, malformed, naming the backend, the record, and the
// field and never the value, which may be a password that begins with env:.
func Record(backend, record, malformed string, names [3]string, get, lookup func(string) (string, bool)) (values [3]string, found bool, failed *credentials.BackendResult) {
	for i, field := range names {
		v, ok := get(field)
		if !ok {
			continue
		}
		found = true
		name, ref, valid := Reference(v)
		switch {
		case !ref:
			values[i] = v
			continue
		case !valid:
			return values, found, &credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: malformed, Message: fmt.Sprintf("credential backend %s: %s %s: env: not followed by a variable name (letters, digits, underscores; no leading digit)", backend, record, field)}
		}
		value, reason := Read(name, lookup)
		if reason != "" {
			missing := Missing(backend, record+" "+field, reason)
			return values, found, &missing
		}
		values[i] = value
	}
	return values, found, nil
}
