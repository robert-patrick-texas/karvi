package secrets

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// TestValueMethodsShowNoSecretByte: no method of a Value that takes no
// argument, Destroy aside, returns three bytes of the secret in a row, of a
// short secret or the tail of a long one.
func TestValueMethodsShowNoSecretByte(t *testing.T) {
	for _, secret := range []string{"x9q", "k4z", "Qz7-pw8x9"} {
		v := New(secret)
		typ := reflect.TypeOf(v)
		for i := 0; i < typ.NumMethod(); i++ {
			m := typ.Method(i)
			if m.Type.NumIn() != 1 || m.Name == "Destroy" {
				continue
			}
			for _, out := range m.Func.Call([]reflect.Value{reflect.ValueOf(v)}) {
				shown := fmt.Sprint(out.Interface())
				for j := 0; j+3 <= len(secret); j++ {
					if strings.Contains(shown, secret[j:j+3]) {
						t.Errorf("%q: %s returns %q, which holds %q", secret, m.Name, shown, secret[j:j+3])
					}
				}
			}
		}
	}
}
