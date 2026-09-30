// Package env implements an explicitly configured operator-keyed environment
// backend. It is distinct from the fixed NETUSER/NETPASS/NETENABLE fallback.
package env

import (
	"context"
	"fmt"
	"os"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
)

type Backend struct {
	BackendName      string
	UsernameTemplate string
	PasswordTemplate string
	EnableTemplate   string
	UsernameIndirect bool
	PasswordIndirect bool
	EnableIndirect   bool
}

func (b *Backend) Name() string                { return b.BackendName }
func (*Backend) Mode() credentials.BackendMode { return credentials.OperatorKeyed }
func (b *Backend) Resolve(_ context.Context, req credentials.ResolveRequest) credentials.BackendResult {
	userVar := format(b.UsernameTemplate, req.Operator.Username)
	passVar := format(b.PasswordTemplate, req.Operator.Username)
	enableVar := format(b.EnableTemplate, req.Operator.Username)
	username, uok := lookup(userVar)
	password, pok := lookup(passVar)
	enable, eok := lookup(enableVar)
	if !uok && !pok && !eok {
		return credentials.BackendResult{Outcome: credentials.NotFound}
	}
	usrc, psrc, esrc := "env:"+userVar, "env:"+passVar, "env:"+enableVar
	username, usrc = indirect(username, b.UsernameIndirect, usrc)
	password, psrc = indirect(password, b.PasswordIndirect, psrc)
	enable, esrc = indirect(enable, b.EnableIndirect, esrc)
	material := secrets.NewMaterial(username, password, enable)
	cred := credentials.Credential{Material: material, Backend: b.BackendName, Policy: req.Policy, MatchedOn: credentials.Match{Category: "operator", SafeValue: req.Operator.Username, Source: "environment"}, FieldSources: map[string]credentials.FieldSource{"username": {Backend: b.BackendName, Path: usrc}, "password": {Backend: b.BackendName, Path: psrc}, "enable_password": {Backend: b.BackendName, Path: esrc}}}
	return credentials.BackendResult{Outcome: credentials.Success, Credential: cred}
}
func format(t, key string) string {
	if t == "" {
		return ""
	}
	if containsPercent(t) {
		return fmt.Sprintf(t, key)
	}
	return t
}
func containsPercent(s string) bool {
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '%' && s[i+1] == 's' {
			return true
		}
	}
	return false
}
func lookup(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	return os.LookupEnv(name)
}
func indirect(v string, on bool, src string) (string, string) {
	if !on {
		return v, src
	}
	if x, ok := os.LookupEnv(v); ok {
		return x, "env:" + v
	}
	return v, "literal"
}
