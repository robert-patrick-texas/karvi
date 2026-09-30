package canary

import (
	"bytes"
	"encoding"
	"encoding/gob"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"reflect"
	"text/template"
)

// Profile says what the sinks must do with a value.
type Profile int

const (
	// Refusing: every encoder returns an error and every formatter redacts
	// (SecretString, SecretBytes, CredentialGrant, CredentialPackage).
	Refusing Profile = iota
	// Capability: JSON, text, and binary encoding succeed because the value
	// must cross the IPC envelope; fmt, slog, and templates still redact
	// (ChannelToken).
	Capability
	// Hidden: a container holding a secret in an unexported field. Encoders
	// cannot see the field, so nothing is required of them; every output is
	// still scanned.
	Hidden
)

// Exercise drives value through every encoding sink and
// returns one finding per violation: an encoder that succeeded when it had
// to refuse, an encoder that failed when it had to succeed, or a canary
// byte in any output. An empty result is the proof.
func Exercise(value any, profile Profile, seeds ...Value) []string {
	var findings []string
	leak := func(sink string, out []byte) {
		for _, h := range Scan(sink, out, seeds...) {
			findings = append(findings, fmt.Sprintf("%s: canary found (%s at %d) in %q", sink, h.Encoding, h.Offset, excerpt(out, h.Offset)))
		}
	}
	// An encoder sink is one a capability is allowed to feed: its output is
	// the value by design, so under Capability it is not scanned.
	mustRefuse := func(sink string, out []byte, err error) {
		if profile != Capability {
			leak(sink, out)
		}
		if profile == Refusing && err == nil {
			findings = append(findings, sink+": encoder succeeded; it must refuse")
		}
	}
	mustSucceed := func(sink string, err error) {
		if profile == Capability && err != nil {
			findings = append(findings, sink+": encoder failed; a capability must encode: "+err.Error())
		}
	}
	ptr := reflect.New(reflect.TypeOf(value))
	ptr.Elem().Set(reflect.ValueOf(value))

	// encoding/json in every container.
	for name, v := range map[string]any{
		"json.Marshal(value)":   value,
		"json.Marshal(pointer)": ptr.Interface(),
		"json.Marshal(struct)": struct {
			V any `json:"v"`
		}{value},
		"json.Marshal(map)":   map[string]any{"v": value},
		"json.Marshal(slice)": []any{value},
	} {
		out, err := json.Marshal(v)
		mustRefuse(name, out, err)
		if name == "json.Marshal(value)" {
			mustSucceed(name, err)
		}
	}
	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(value)
	mustRefuse("json.Encoder", buf.Bytes(), err)

	// encoding.TextMarshaler, TextAppender, BinaryMarshaler.
	if m, ok := value.(encoding.TextMarshaler); ok {
		out, err := m.MarshalText()
		mustRefuse("MarshalText", out, err)
		mustSucceed("MarshalText", err)
	}
	if m, ok := value.(interface{ AppendText([]byte) ([]byte, error) }); ok {
		out, err := m.AppendText(nil)
		mustRefuse("AppendText", out, err)
		mustSucceed("AppendText", err)
	}
	if m, ok := value.(encoding.BinaryMarshaler); ok {
		out, err := m.MarshalBinary()
		mustRefuse("MarshalBinary", out, err)
		mustSucceed("MarshalBinary", err)
	}

	// encoding/gob and encoding/xml.
	buf.Reset()
	err = gob.NewEncoder(&buf).Encode(value)
	mustRefuse("gob", buf.Bytes(), err)
	out, _ := xml.Marshal(value)
	if profile != Capability {
		leak("xml", out)
	}

	// fmt: every verb, on the value, a pointer, containers, and a wrapper.
	subjects := map[string]any{"value": value, "pointer": ptr.Interface(), "struct": struct{ V any }{value}, "slice": []any{value}, "map": map[string]any{"k": value}}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%T", "%p"} {
		for name, s := range subjects {
			leak("fmt "+verb+" "+name, []byte(fmt.Sprintf(verb, s)))
		}
	}
	leak("fmt.Sprint", []byte(fmt.Sprint(value)))
	leak("fmt.Sprintln", []byte(fmt.Sprintln(value)))
	leak("fmt.Errorf", []byte(fmt.Errorf("wrapped: %v", value).Error()))

	// log/slog through both handlers, as an attribute and inside a group.
	for name, mk := range map[string]func(*bytes.Buffer) slog.Handler{
		"slog.TextHandler": func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
		"slog.JSONHandler": func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
	} {
		var b bytes.Buffer
		logger := slog.New(mk(&b))
		logger.Info("event", "v", value, slog.Any("a", value), slog.Group("g", "v", value))
		logger.With("w", value).Info("with")
		leak(name, b.Bytes())
	}

	// text/template: the value, every fmt verb, every exported field, and
	// every zero-argument exported method except Destroy. Execution errors
	// are not findings; only output is.
	sources := []string{`{{.}}`, `{{printf "%v" .}}`, `{{printf "%+v" .}}`, `{{printf "%#v" .}}`, `{{printf "%s" .}}`, `{{printf "%x" .}}`}
	rt := reflect.TypeOf(value)
	if rt.Kind() == reflect.Struct {
		for i := 0; i < rt.NumField(); i++ {
			if f := rt.Field(i); f.IsExported() {
				sources = append(sources, "{{."+f.Name+"}}", `{{printf "%v" .`+f.Name+`}}`)
			}
		}
	}
	encoderMethods := map[string]bool{"MarshalJSON": true, "MarshalText": true, "MarshalBinary": true, "AppendText": true, "GobEncode": true}
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		if m.Name == "Destroy" || m.Type.NumIn() != 1 || m.Type.NumOut() < 1 {
			continue
		}
		if profile == Capability && encoderMethods[m.Name] {
			continue
		}
		sources = append(sources, "{{."+m.Name+"}}")
	}
	for _, src := range sources {
		tmpl, err := template.New("t").Parse(src)
		if err != nil {
			continue
		}
		var b bytes.Buffer
		_ = tmpl.Execute(&b, value)
		leak("template "+src, b.Bytes())
	}
	return findings
}

// excerpt returns a bounded window of out around offset for a finding.
func excerpt(out []byte, offset int) string {
	start, end := offset-40, offset+60
	if start < 0 {
		start = 0
	}
	if end > len(out) {
		end = len(out)
	}
	return string(out[start:end])
}
