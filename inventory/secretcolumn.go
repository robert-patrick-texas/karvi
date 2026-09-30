package inventory

import (
	"strings"
	"unicode"
)

// secretColumnWords are the words that mark a column as holding a secret.
// The list is fixed: the rule is a hard refusal with no override, so it must
// mean the same thing on every installation.
var secretColumnWords = []string{"password", "passwd", "secret", "token", "private_key", "passphrase"}

// SecretColumnWord reports whether a column or attribute name marks a
// secret: after normalisation (white space trimmed, lower case, a hyphen or
// an inner space read as an underscore) the name equals or ends with one of
// the listed words, so `password`, `Enable-Password`, `api_token`, and
// `ssh_private_key` are secret names and `token_ring` and `credkeyref` are
// not. It returns the word that matched, which a message can print instead
// of the name itself.
//
// An inventory file holds no secret column. The loader asks this of every
// header of a header-mode file, mapped or not (an unmapped column is where
// a stray secret would sit), and configuration validation asks it of every
// attribute mapping's name, the one name a numeric-mode file has. The
// remedy is a credential CSV (`type = "csv"`) and deleting the column.
func SecretColumnWord(name string) (string, bool) {
	canonical := strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return '_'
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(name))
	for _, word := range secretColumnWords {
		if strings.HasSuffix(canonical, word) {
			return word, true
		}
	}
	return "", false
}
