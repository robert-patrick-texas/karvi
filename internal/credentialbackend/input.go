package credentialbackend

import (
	"context"
	"fmt"
	"os"

	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// Prompt fields of the built-in fallback.
const (
	FieldUsername       = "username"
	FieldPassword       = "password"
	FieldEnablePassword = "enable_password"
)

// PromptRequest is one field the built-in fallback needs from the operator.
type PromptRequest struct {
	// Field is username, password, or enable_password.
	Field string
	// Masked says the answer must not echo.
	Masked bool
	// Target is the device the resolver is working on; a provider that
	// prompts once for many targets may name the count instead.
	Target string
}

// Label is the operator-facing name of the field.
func (r PromptRequest) Label() string {
	switch r.Field {
	case FieldUsername:
		return "Username"
	case FieldEnablePassword:
		return "Enable password"
	default:
		return "Password"
	}
}

// InputProvider is the shape of the built-in fallback's input: where it reads
// NETUSER, NETPASS, and NETENABLE, and how it asks for what is missing. It is
// local to the submitting client; the daemon supplies none, so a resolver
// without a provider has no environment and no terminal.
type InputProvider interface {
	LookupEnv(ctx context.Context, name string) (string, bool, error)
	Prompt(ctx context.Context, req PromptRequest) (string, error)
}

// TTYInput is the process environment and the controlling terminal, the
// provider command, login, and the client planner use.
type TTYInput struct{}

func (TTYInput) LookupEnv(_ context.Context, name string) (string, bool, error) {
	v, ok := os.LookupEnv(name)
	return v, ok, nil
}

func (TTYInput) Prompt(_ context.Context, req PromptRequest) (string, error) {
	return osutil.ReadTTY(fmt.Sprintf("%s for %s: ", req.Label(), req.Target), req.Masked)
}

// SetInput replaces the provider; nil disables the environment fallback and
// every prompt, which is the daemon's configuration.
func (r *Resolver) SetInput(p InputProvider) { r.input = p }

// RegisterBackend adds or replaces a backend adapter under name, for adapters
// built outside the configuration table and for tests.
func (r *Resolver) RegisterBackend(name string, b credentialsBackend) { r.backends[name] = b }
