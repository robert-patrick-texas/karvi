// Package envindirect is the shared environment-indirection helper for
// credential backends (the env-indirection.username, .password, and
// .enable-password flags). With a
// field's flag on, a stored value that names a set environment variable is
// replaced by that variable's value. The env, Redis, and Vault backends
// each hold a private copy of this rule; folding them into this package is
// a clean-up roadmap line, and new backends use this one.
package envindirect

import "os"

// Lookup resolves one field. When on is false, or value is blank, or value
// names no set variable, it returns value unchanged and ok false, and the
// caller keeps its own field source. Otherwise it returns the variable's
// value, the variable's name for the field source ("env:"+name), and ok
// true. lookup is os.LookupEnv when nil; a test passes its own.
//
// A variable that is set but empty counts as set: the field becomes blank
// and the ordinary completeness rules judge the result, as they judge a
// blank cell.
func Lookup(value string, on bool, lookup func(string) (string, bool)) (resolved, name string, ok bool) {
	if !on || value == "" {
		return value, "", false
	}
	if lookup == nil {
		lookup = os.LookupEnv
	}
	if v, set := lookup(value); set {
		return v, value, true
	}
	return value, "", false
}

// Source is the field source for a value Lookup replaced.
func Source(name string) string { return "env:" + name }
