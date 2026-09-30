package credentials

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/canary"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
)

type secretsValueAlias = secrets.Value

// The shape is the guarantee: one pointer field and
// every method on the value receiver.
func TestSecretTypesHaveThePointerShape(t *testing.T) {
	for _, rt := range []reflect.Type{reflect.TypeOf(SecretString{}), reflect.TypeOf(SecretBytes{})} {
		if rt.NumField() != 1 || rt.Field(0).Type.Kind() != reflect.Pointer {
			t.Errorf("%s must hold exactly one pointer field", rt)
		}
		if rt.Field(0).IsExported() {
			t.Errorf("%s field must be unexported", rt)
		}
		value, pointer := rt.NumMethod(), reflect.PointerTo(rt).NumMethod()
		if value != pointer {
			t.Errorf("%s has %d value methods and %d pointer methods; every method must use the value receiver", rt, value, pointer)
		}
		for _, name := range []string{"String", "GoString", "Format", "LogValue", "MarshalJSON", "MarshalText", "AppendText", "GobEncode", "MarshalBinary", "WithBytes", "IsSet", "Len", "Equal", "Destroy"} {
			if _, ok := rt.MethodByName(name); !ok {
				t.Errorf("%s lacks %s", rt, name)
			}
		}
		if _, ok := rt.MethodByName("Hint"); ok {
			t.Errorf("%s must not export Hint", rt)
		}
	}
}

func TestSecretStringRefusesEverySink(t *testing.T) {
	seed := canarytest.Seed(t)
	s := NewSecretString(seed.Raw)
	canarytest.Exercise(t, s, canary.Refusing, seed)
	canarytest.Exercise(t, struct{ S SecretString }{s}, canary.Refusing, seed)
	// Unexported fields: encoders cannot see them; fmt's bad-verb path for
	// %s and %q dumps the pointee, which is why the internal value keeps its
	// bytes behind a second pointer.
	canarytest.Exercise(t, struct{ s SecretString }{s}, canary.Hidden, seed)
	canarytest.Exercise(t, struct{ v *secretsValueAlias }{secrets.New(seed.Raw)}, canary.Hidden, seed)
	canarytest.Exercise(t, struct{ Nested struct{ S SecretString } }{struct{ S SecretString }{s}}, canary.Refusing, seed)
	canarytest.Exercise(t, []SecretString{s}, canary.Refusing, seed)
	b := NewSecretBytes([]byte(seed.Raw))
	canarytest.Exercise(t, b, canary.Refusing, seed)
	canarytest.Exercise(t, struct{ b SecretBytes }{b}, canary.Hidden, seed)
	// The refusal carries the registered code, addressable or not.
	_, err := json.Marshal(struct{ S SecretString }{s})
	if errorcodes.Of(err) != "secret_serialization_refused" {
		t.Errorf("json error code %q: %v", errorcodes.Of(err), err)
	}
	_, err = json.Marshal(&s)
	if errorcodes.Of(err) != "secret_serialization_refused" {
		t.Errorf("json pointer error code %q: %v", errorcodes.Of(err), err)
	}
	if s.String() != "<redacted>" || !strings.Contains(s.GoString(), "redacted") {
		t.Error("String or GoString is not redacted")
	}
}

func TestSecretStringSemantics(t *testing.T) {
	a, b, c := NewSecretString("same"), NewSecretString("same"), NewSecretString("other")
	var unset SecretString
	if !a.Equal(b) || a.Equal(c) || a.Equal(unset) || !unset.Equal(SecretString{}) {
		t.Error("Equal semantics")
	}
	if !a.IsSet() || a.Len() != 4 || unset.IsSet() || unset.Len() != 0 {
		t.Error("IsSet or Len")
	}
	var seen string
	if err := a.WithBytes(func(p []byte) error { seen = string(p); p[0] = 'X'; return nil }); err != nil || seen != "same" {
		t.Fatalf("WithBytes: %v %q", err, seen)
	}
	if !a.Equal(b) {
		t.Error("mutating the callback copy changed the value")
	}
	if err := unset.WithBytes(func([]byte) error { return nil }); errorcodes.Of(err) != "secret_unset" {
		t.Errorf("unset WithBytes: %v", err)
	}
	// Destroy is shared across copies.
	copy1, copy2 := a, a
	copy1.Destroy()
	if copy2.IsSet() || a.IsSet() {
		t.Error("a copy still reads after Destroy")
	}
	if err := copy2.WithBytes(func([]byte) error { return nil }); errorcodes.Of(err) != "secret_destroyed" {
		t.Errorf("destroyed WithBytes: %v", err)
	}
	if !copy2.Equal(unset) {
		t.Error("a destroyed value must equal an unset one")
	}
}
