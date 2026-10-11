// Package env implements an explicitly configured operator-keyed environment
// backend. It is distinct from the fixed NETUSER/NETPASS/NETENABLE fallback.
package env

import (
	"context"
	"fmt"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/envname"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
)

type Backend struct {
	BackendName      string
	UsernameTemplate string
	PasswordTemplate string
	EnableTemplate   string
}

func (b *Backend) Name() string                { return b.BackendName }
func (*Backend) Mode() credentials.BackendMode { return credentials.OperatorKeyed }

// Resolve reads the three variables the templates name, each %operator%
// filled with the operator's name after the backend's transform, through
// the request's lookup; a template "" names none. A variable's value is the
// field's, as written. The backend answers when any variable is set and is
// not found when none is. A filled name that is not a portable variable name
// stops the device, since no shell can export it, naming the remedy.
func (b *Backend) Resolve(_ context.Context, req credentials.ResolveRequest) credentials.BackendResult {
	var values [3]string
	found := false
	for i, t := range []struct{ key, template string }{
		{"username-var-template", b.UsernameTemplate},
		{"password-var-template", b.PasswordTemplate},
		{"enable-var-template", b.EnableTemplate},
	} {
		if t.template == "" {
			continue
		}
		name := req.Fill(t.template)
		if !envname.Portable(name) {
			return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: "config_credential_backend_variable_invalid", Message: fmt.Sprintf("credential backend %s: %s gives %q, not a variable name (letters, digits, underscores; no leading digit); remedy: replace-chars in the backend's operator transform", b.BackendName, t.key, name)}
		}
		if req.LookupEnv == nil {
			continue
		}
		if v, ok := req.LookupEnv(name); ok {
			values[i], found = v, true
		}
	}
	if !found {
		return credentials.BackendResult{Outcome: credentials.NotFound}
	}
	material := secrets.NewMaterial(values[0], values[1], values[2])
	cred := credentials.Credential{Material: material, Backend: b.BackendName, Policy: req.Policy, MatchedOn: credentials.Match{Category: "operator", SafeValue: req.Operator.Username, Source: "environment"}}
	return credentials.BackendResult{Outcome: credentials.Success, Credential: cred}
}
