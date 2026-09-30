package canary

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func TestScanFindsEveryEncoding(t *testing.T) {
	v := New()
	raw := []byte(v.Raw)
	cases := map[string][]byte{
		"raw":       []byte("password=" + v.Raw + "\n"),
		"hex":       []byte("blob:" + hex.EncodeToString(append([]byte("xy"), raw...))),
		"HEX":       []byte(strings.ToUpper(hex.EncodeToString(raw))),
		"fmt-bytes": []byte(fmt.Sprintf("%v", struct{ P []byte }{append([]byte("zz"), raw...)})),
	}
	for name, data := range cases {
		hits := Scan(name, data, v)
		found := false
		for _, h := range hits {
			if strings.HasPrefix(h.Encoding, name) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no hit in %q (hits %v)", name, data, hits)
		}
	}
	// base64 at every alignment, both alphabets, with and without padding.
	for shift := 0; shift < 5; shift++ {
		prefix := []byte(strings.Repeat("Q", shift))
		for name, enc := range map[string]*base64.Encoding{"std": base64.StdEncoding, "url": base64.URLEncoding, "rawstd": base64.RawStdEncoding} {
			data := []byte(enc.EncodeToString(append(append(prefix, raw...), []byte("tail")...)))
			if len(Scan("b64", data, v)) == 0 {
				t.Errorf("base64 %s shift %d: not found in %s", name, shift, data)
			}
		}
	}
	// The masked form is not a hit, nor is a different canary.
	masked := strings.Repeat("*", len(v.Raw)-4) + v.Raw[len(v.Raw)-4:]
	if len(Scan("masked", []byte(masked), v)) != 0 {
		t.Error("masked form reported as a hit")
	}
	if len(Scan("other", []byte(New().Raw), v)) != 0 {
		t.Error("a different canary reported as a hit")
	}
	// Binary data with the canary mid-stream reports the offset.
	bin := append(append([]byte{0, 1, 2, 255}, raw...), 0, 0)
	hits := Scan("bin", bin, v)
	if len(hits) != 1 || hits[0].Offset != 4 || hits[0].Encoding != "raw" {
		t.Errorf("binary: %v", hits)
	}
}

// leaky implements String by returning its secret and lets JSON through:
// the harness must report both.
type leaky struct{ Secret string }

func (l leaky) String() string { return l.Secret }

// clean keeps its secret behind a pointer: fmt's bad-verb path (%p on a
// non-pointer) skips every method and prints fields by reflection, so a
// plain string field would leak there. That is the redaction rule in
// miniature.
type clean struct{ secret *string }

func (c clean) String() string               { return "<redacted>" }
func (c clean) GoString() string             { return "<redacted>" }
func (c clean) Format(f fmt.State, _ rune)   { fmt.Fprint(f, "<redacted>") }
func (c clean) MarshalJSON() ([]byte, error) { return nil, fmt.Errorf("refused") }
func (c clean) MarshalText() ([]byte, error) { return nil, fmt.Errorf("refused") }
func (c clean) GobEncode() ([]byte, error)   { return nil, fmt.Errorf("refused") }

func TestExerciseCatchesALeakAndPassesAClean(t *testing.T) {
	v := New()
	findings := Exercise(leaky{Secret: v.Raw}, Refusing, v)
	if len(findings) == 0 {
		t.Fatal("the leaky type produced no findings")
	}
	var sawFmt, sawJSON, sawTemplate, sawSlog bool
	for _, f := range findings {
		sawFmt = sawFmt || strings.HasPrefix(f, "fmt ")
		sawJSON = sawJSON || strings.HasPrefix(f, "json.Marshal(value): canary") || strings.HasPrefix(f, "json.Marshal(value): encoder succeeded")
		sawTemplate = sawTemplate || strings.HasPrefix(f, "template ")
		sawSlog = sawSlog || strings.HasPrefix(f, "slog.")
	}
	if !sawFmt || !sawJSON || !sawTemplate || !sawSlog {
		t.Errorf("findings miss a sink (fmt %v json %v template %v slog %v):\n%s", sawFmt, sawJSON, sawTemplate, sawSlog, strings.Join(findings, "\n"))
	}
	raw := v.Raw
	if findings := Exercise(clean{secret: &raw}, Refusing, v); len(findings) != 0 {
		t.Errorf("the clean type produced findings:\n%s", strings.Join(findings, "\n"))
	}
}
