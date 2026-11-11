// Package testprompts holds the keyboard-interactive shapes both SSH
// transports' tests run against the fake device, so the two answer alike.
package testprompts

import "github.com/robert-patrick-texas/karvi/internal/fakedevice"

// Row is one shape: the fake's prompts, the failure's message (empty for a
// login), and what the device received (fakedevice.Server.KbdAnswers,
// joined by |).
type Row struct {
	Name     string
	Prompts  []fakedevice.KbdPrompt
	Err, Got string
}

// Rows: each field at most once a connection, a prompt asking for one a
// second time, or a change prompt at any position, ending the connection at
// that prompt, the device getting nothing in its round and no
// password-method attempt after it.
var Rows = []Row{
	{"username then password", []fakedevice.KbdPrompt{{Prompt: "Username: ", Want: "username"}, {Prompt: "Password: ", Want: "password"}},
		"", `"Username: " username|"Password: " password`},
	{"username, then password in a new round", []fakedevice.KbdPrompt{{Prompt: "Username: ", Want: "username"}, {Prompt: "Password: ", Want: "password", NewRound: true}},
		"", `"Username: " username|"Password: " password`},
	{"a refused password", []fakedevice.KbdPrompt{{Prompt: "Password: ", Want: "username"}},
		`authentication_failed: device refused password`, `"Password: " password`},
	{"the password twice in a round", []fakedevice.KbdPrompt{{Prompt: "Password: ", Want: "password"}, {Prompt: "Password: ", Want: "password"}},
		`authentication_failed: device refused password`, ``},
	{"the password again in a new round", []fakedevice.KbdPrompt{{Prompt: "Password: ", Want: "password"}, {Prompt: "Passcode or option (1-3): ", Want: "password", NewRound: true}},
		`authentication_failed: device refused password`, `"Password: " password`},
	{"the username again in a new round", []fakedevice.KbdPrompt{{Prompt: "Username: ", Want: "username"}, {Prompt: "login as: ", Want: "username", NewRound: true}},
		`authentication_failed: device refused username`, `"Username: " username`},
	{"a change after an answer in a round", []fakedevice.KbdPrompt{{Prompt: "Password: ", Want: "password"}, {Prompt: "New password: ", Want: "empty"}},
		`authentication_change_requested: the device asks to change a secret: "New password: "`, ``},
	{"a new PIN round after the passcode", []fakedevice.KbdPrompt{{Prompt: "Enter PASSCODE: ", Want: "password"}, {Prompt: "Enter new PIN: ", Want: "empty", NewRound: true}},
		`authentication_change_requested: the device asks to change a secret: "Enter new PIN: "`, `"Enter PASSCODE: " password`},
}
