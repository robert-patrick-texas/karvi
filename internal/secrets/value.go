// Package secrets contains the concrete secret-bearing implementation. It is
// intentionally internal and refuses ordinary formatting and serialization.
package secrets

import (
	"log/slog"
	"strings"
	"sync"

	"github.com/robert-patrick-texas/karvi/internal/canary"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

var ErrDestroyed = errorcodes.Errorf("secret_destroyed", "secret value has been destroyed")

// Value keeps its bytes behind a second pointer on purpose. fmt's bad-verb
// path (%s or %q reaching an unexported *Value field through reflection)
// dereferences one pointer level and dumps the pointee's fields with %v; a
// nested pointer at that depth prints as an address, so the bytes never
// appear.
type Value struct {
	mu sync.RWMutex
	b  *buffer
}

type buffer struct {
	data      []byte
	destroyed bool
}

func New(s string) *Value      { return NewBytes([]byte(s)) }
func NewBytes(b []byte) *Value { return &Value{b: &buffer{data: append([]byte(nil), b...)}} }
func (v *Value) IsSet() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return !v.b.destroyed && len(v.b.data) > 0
}
func (v *Value) Len() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.b.destroyed {
		return 0
	}
	return len(v.b.data)
}
func (v *Value) String() string       { return "<redacted>" }
func (v *Value) GoString() string     { return "<redacted>" }
func (v *Value) LogValue() slog.Value { return slog.StringValue("<redacted>") }
func (v *Value) MarshalJSON() ([]byte, error) {
	return nil, errorcodes.Errorf("secret_serialization_refused", "secret values cannot be JSON encoded")
}
func (v *Value) MarshalText() ([]byte, error) {
	return nil, errorcodes.Errorf("secret_serialization_refused", "secret values cannot be text encoded")
}
func (v *Value) GobEncode() ([]byte, error) {
	return nil, errorcodes.Errorf("secret_serialization_refused", "secret values cannot be gob encoded")
}

func (v *Value) WithBytes(fn func([]byte) error) error {
	v.mu.RLock()
	if v.b.destroyed {
		v.mu.RUnlock()
		return ErrDestroyed
	}
	copyValue := append([]byte(nil), v.b.data...)
	v.mu.RUnlock()
	defer wipe(copyValue)
	return fn(copyValue)
}

func (v *Value) Hint() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.b.destroyed || len(v.b.data) == 0 {
		return "<unset>"
	}
	stars := make([]byte, len(v.b.data))
	for i := range stars {
		stars[i] = '*'
	}
	tail := v.b.data
	if len(tail) > 4 {
		tail = tail[len(tail)-4:]
	}
	return string(stars) + string(tail)
}

func (v *Value) Destroy() {
	v.mu.Lock()
	defer v.mu.Unlock()
	wipe(v.b.data)
	v.b.data = nil
	v.b.destroyed = true
}
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// Material is the concrete implementation of credentials.Material.
type Material struct{ Username, Password, EnablePassword *Value }

func NewMaterial(username, password, enable string) *Material {
	return &Material{New(username), New(password), New(enable)}
}
func (m *Material) UsernameSet() bool { return m != nil && m.Username != nil && m.Username.IsSet() }
func (m *Material) PasswordSet() bool { return m != nil && m.Password != nil && m.Password.IsSet() }
func (m *Material) EnablePasswordSet() bool {
	return m != nil && m.EnablePassword != nil && m.EnablePassword.IsSet()
}
func (m *Material) WithUsername(fn func([]byte) error) error {
	if m == nil || m.Username == nil {
		return errorcodes.Errorf("secret_username_unset", "username is unset")
	}
	return m.Username.WithBytes(fn)
}
func (m *Material) WithPassword(fn func([]byte) error) error {
	if m == nil || m.Password == nil {
		return errorcodes.Errorf("secret_password_unset", "password is unset")
	}
	return m.Password.WithBytes(fn)
}
func (m *Material) WithEnablePassword(fn func([]byte) error) error {
	if m == nil || m.EnablePassword == nil {
		return errorcodes.Errorf("secret_enable_password_unset", "enable password is unset")
	}
	return m.EnablePassword.WithBytes(fn)
}
func (m *Material) Destroy() {
	if m == nil {
		return
	}
	if m.Username != nil {
		m.Username.Destroy()
	}
	if m.Password != nil {
		m.Password.Destroy()
	}
	if m.EnablePassword != nil {
		m.EnablePassword.Destroy()
	}
}

// AssertNonSerializable is used in tests to protect the security boundary.
// It delegates to the canary harness: every sink must refuse or redact, not
// only JSON.
func AssertNonSerializable(v any) error {
	if findings := canary.Exercise(v, canary.Refusing); len(findings) > 0 {
		return errorcodes.Errorf("secret_serialized_unexpectedly", "value reached a sink: %s", strings.Join(findings, "; "))
	}
	return nil
}
