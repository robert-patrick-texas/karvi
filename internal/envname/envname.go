// Package envname is the portable variable name: a letter or an underscore,
// then letters, digits, and underscores. Every name karvi reads a variable
// by, from a credential CSV's env:NAME cell, a backend's configuration, or
// the credential helper's output, is one; the package imports nothing of
// karvi's, so the loader and the job runner may share it with the
// credential readers.
package envname

// Portable reports whether name is a portable variable name.
func Portable(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		letter := r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		if !letter && (i == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return true
}
