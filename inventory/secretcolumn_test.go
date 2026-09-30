package inventory

import "testing"

// TestSecretColumnWord covers the secret-column word list: a name that equals
// or ends with a listed word, after the reader's normalisation, marks a
// secret; a name that merely contains one does not.
func TestSecretColumnWord(t *testing.T) {
	for name, want := range map[string]string{
		"password": "password", "Password": "password", " PASSWORD ": "password",
		"enable_password": "password", "Enable-Password": "password", "enable password": "password", "enablepassword": "password",
		"passwd": "passwd", "snmp_secret": "secret", "api_token": "token", "API-Token": "token",
		"ssh_private_key": "private_key", "private-key": "private_key", "Private Key": "private_key", "key_passphrase": "passphrase",
		// Not secret names: the word is not at the end, or is not there.
		"token_ring": "", "password_age": "", "secret_level": "", "credkeyref": "", "credkey": "",
		"name": "", "site": "", "key": "", "public_key": "", "": "",
	} {
		word, secret := SecretColumnWord(name)
		if word != want || secret != (want != "") {
			t.Errorf("%q: word %q secret %v, want %q", name, word, secret, want)
		}
	}
}
