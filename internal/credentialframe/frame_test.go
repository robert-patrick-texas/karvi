package credentialframe

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/canary"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

const packageID = "20260914T120005.000000+0000-0123456789abcdefghjk"

func fixturePackage(password string) credentialpackage.CredentialPackage {
	final := plantest.FinalPlan()
	window := func(g *credentialpackage.CredentialGrant) {
		g.NotBefore, g.NotAfter = plantest.DraftedAt, plantest.DraftedAt.Add(12*time.Hour)
	}
	a := credentialpackage.CredentialGrant{CredentialID: plantest.GrantA, Method: credentialpackage.MethodEmbeddedSecret, Username: credentials.NewSecretString("u"), Password: credentials.NewSecretString(password), Policy: "default", Backend: "env", MatchedOn: credentials.Match{Category: "operator", SafeValue: "netops"}, Scope: credentialpackage.CredentialScope{TargetIDs: []string{"name:127.0.0.1", "name:edge-b"}, Transports: []string{"system"}, Ports: []uint16{22}}}
	b := credentialpackage.CredentialGrant{CredentialID: plantest.GrantB, Method: credentialpackage.MethodEmbeddedSecret, Username: credentials.NewSecretString("admin"), Password: credentials.NewSecretString("q"), Policy: "core", Backend: "cloginrc", MatchedOn: credentials.Match{Category: "device", Pattern: "core-*"}, Scope: credentialpackage.CredentialScope{TargetIDs: []string{"name:core-a"}, Transports: []string{"system"}, Ports: []uint16{22}}}
	window(&a)
	window(&b)
	return credentialpackage.CredentialPackage{
		SchemaVersion: 1, PackageID: packageID, JobID: plantest.JobID, PlanDigest: final.PlanDigest,
		Issuer: credentialpackage.Principal{Kind: "operator", Username: "netops", UID: 1000, Hostname: "ops01"}, Audience: []string{"daemon:ops01:1000"},
		Protection: credentialpackage.ProtectionLocalPeer, IssuedAt: plantest.FinalizedAt, ExpiresAt: plantest.FinalizedAt.Add(10 * time.Minute),
		Grants:   []credentialpackage.CredentialGrant{a, b},
		Bindings: []credentialpackage.TargetCredentialBinding{{TargetID: "name:127.0.0.1", CredentialID: plantest.GrantA}, {TargetID: "name:edge-b", CredentialID: plantest.GrantA}, {TargetID: "name:core-a", CredentialID: plantest.GrantB}},
	}
}

func fixtureFrame(t *testing.T, password string) (Header, credentials.SecretBytes, ipc.ChannelToken) {
	t.Helper()
	pkg := fixturePackage(password)
	proj, err := pkg.SafeProjection()
	if err != nil {
		t.Fatal(err)
	}
	protector, _ := credentialpackage.ProtectorFor(credentialpackage.ProtectionLocalPeer)
	protected, err := protector.Protect(context.Background(), pkg)
	if err != nil {
		t.Fatal(err)
	}
	if protected.Envelope.PackageDigest != proj.PackageDigest {
		t.Fatal("protected envelope is not the projection's")
	}
	tok, _ := ipc.NewChannelToken()
	return Header{PreparationID: plantest.PreparationID, Token: tok, Envelope: protected.Envelope}, protected.Body, tok
}

func TestFrameRoundTripAndLayout(t *testing.T) {
	seed := canarytest.Seed(t)
	h, body, tok := fixtureFrame(t, seed.Raw)
	var buf bytes.Buffer
	if err := Write(&buf, h, body); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	t.Logf("frame prefix: %s (%d bytes total)", hex.EncodeToString(raw[:prefixLen]), len(raw))
	if binary.BigEndian.Uint32(raw[0:4]) != Magic || binary.BigEndian.Uint16(raw[4:6]) != 1 || binary.BigEndian.Uint16(raw[6:8]) != 0 {
		t.Fatalf("prefix %x", raw[:8])
	}
	headerLen := int(binary.BigEndian.Uint32(raw[8:12]))
	bodyLen := int(binary.BigEndian.Uint32(raw[12:16]))
	if prefixLen+headerLen+bodyLen != len(raw) {
		t.Fatalf("lengths %d+%d do not span %d", headerLen, bodyLen, len(raw))
	}
	// The header carries no secret; the body is the only place the canary is.
	if canary.Found(raw[prefixLen:prefixLen+headerLen], seed) {
		t.Fatal("frame header carries the canary")
	}
	if !canary.Found(raw[prefixLen+headerLen:], seed) {
		t.Fatal("frame body does not carry the package")
	}
	got, gotBody, err := Read(bytes.NewReader(raw), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got.PreparationID != h.PreparationID || !got.Token.Equal(tok) || got.Envelope.PackageDigest != h.Envelope.PackageDigest {
		t.Fatal("header differs after the round trip")
	}
	if err := got.VerifyToken(tok); err != nil {
		t.Fatal(err)
	}
	other, _ := ipc.NewChannelToken()
	if err := got.VerifyToken(other); errorcodes.Of(err) != "credential_channel_token_invalid" {
		t.Errorf("wrong token: %v", err)
	}
	protector, _ := credentialpackage.ProtectorFor(got.Envelope.Protection)
	pkg, err := protector.Unprotect(context.Background(), credentialpackage.Protected{Envelope: got.Envelope, Body: gotBody}, got.Envelope.Audience[0])
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.Grants[0].Password.Equal(credentials.NewSecretString(seed.Raw)) {
		t.Fatal("password differs after the round trip")
	}
	canarytest.Exercise(t, gotBody, canary.Refusing, seed)
	// The receipt round trip.
	var rbuf bytes.Buffer
	rc := Receipt{PackageID: packageID, PackageDigest: h.Envelope.PackageDigest, GrantCount: 2, BindingCount: 3, Accepted: true}
	if err := WriteReceipt(&rbuf, rc); err != nil {
		t.Fatal(err)
	}
	back, err := ReadReceipt(&rbuf)
	if err != nil || !back.Accepted || back.GrantCount != 2 || back.Findings == nil {
		t.Fatalf("receipt: %+v %v", back, err)
	}
	canarytest.SchemaParityAt(t, "../../schema/daemon-ipc.schema.json", "#/$defs/package_receipt", back)
	canarytest.SchemaParityAt(t, "../../schema/daemon-ipc.schema.json", "#/$defs/credential_frame_header", h)
}

func TestFrameVectors(t *testing.T) {
	h, body, _ := fixtureFrame(t, "p")
	var good bytes.Buffer
	if err := Write(&good, h, body); err != nil {
		t.Fatal(err)
	}
	raw := good.Bytes()
	headerLen := int(binary.BigEndian.Uint32(raw[8:12]))
	mutate := func(edit func([]byte) []byte) []byte { return edit(append([]byte(nil), raw...)) }
	cases := []struct {
		name  string
		data  []byte
		limit Limits
		code  string
	}{
		{"bad magic", mutate(func(b []byte) []byte { b[0] = 'X'; return b }), Limits{}, "credential_frame_malformed"},
		{"version", mutate(func(b []byte) []byte { b[5] = 2; return b }), Limits{}, "credential_frame_malformed"},
		{"flags", mutate(func(b []byte) []byte { b[7] = 1; return b }), Limits{}, "credential_frame_malformed"},
		{"header too large", raw, Limits{Header: headerLen - 1}, "credential_frame_too_large"},
		{"body too large", raw, Limits{Body: 16}, "credential_frame_too_large"},
		{"truncated body", raw[:len(raw)-1], Limits{}, "credential_frame_malformed"},
		{"truncated header", raw[:prefixLen+4], Limits{}, "credential_frame_malformed"},
		{"empty", nil, Limits{}, "credential_frame_malformed"},
		{"declared oversize before allocation", mutate(func(b []byte) []byte { binary.BigEndian.PutUint32(b[12:16], 1<<31); return b[:prefixLen] }), Limits{}, "credential_frame_too_large"},
		{"header unknown field", mutate(func(b []byte) []byte {
			hdr := string(b[prefixLen : prefixLen+headerLen])
			hdr = strings.Replace(hdr, `{"preparation_id"`, `{"extra":1,"preparation_id"`, 1)
			binary.BigEndian.PutUint32(b[8:12], uint32(len(hdr)))
			return append(append(b[:prefixLen:prefixLen], hdr...), b[prefixLen+headerLen:]...)
		}), Limits{}, "credential_frame_malformed"},
	}
	for _, c := range cases {
		_, _, err := Read(bytes.NewReader(c.data), c.limit)
		if errorcodes.Of(err) != c.code {
			t.Errorf("%s: want %s, got %v", c.name, c.code, err)
		}
	}
	// A header naming sealed is refused even though the envelope is valid.
	sealed := h
	sealed.Envelope.Protection = credentialpackage.ProtectionSealed
	sealed.Envelope.Sealed = &credentialpackage.SealedPayload{KeyID: "k", Nonce: []byte("n"), Ciphertext: []byte("c")}
	sealed.Envelope.PackageDigest = executionplan.Sum([]byte("c"))
	var buf bytes.Buffer
	if err := Write(&buf, sealed, body); errorcodes.Of(err) != "credential_package_invalid" {
		t.Errorf("sealed header: %v", err)
	}
	if err := Write(&buf, h, credentials.SecretBytes{}); errorcodes.Of(err) != "credential_frame_malformed" {
		t.Errorf("empty body: %v", err)
	}
}

// TestCredentialSocketCarriesOnlyTheFrame runs the frame over a Unix socket
// with the peer-UID check the daemon will make, and shows the canary never
// leaves the frame body: not the header, not the receipt.
func TestCredentialSocketCarriesOnlyTheFrame(t *testing.T) {
	seed := canarytest.Seed(t)
	h, body, tok := fixtureFrame(t, seed.Raw)
	dir := testsocket.Dir(t)
	path := filepath.Join(dir, "credentials.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	type outcome struct {
		header  Header
		receipt []byte
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		conn, err := listener.AcceptUnix()
		if err != nil {
			done <- outcome{err: err}
			return
		}
		defer conn.Close()
		if _, err := ipc.PeerUID(conn); err != nil {
			done <- outcome{err: err}
			return
		}
		got, gotBody, err := Read(conn, Limits{})
		if err != nil {
			done <- outcome{err: err}
			return
		}
		if err := got.VerifyToken(tok); err != nil {
			done <- outcome{err: err}
			return
		}
		protector, _ := credentialpackage.ProtectorFor(got.Envelope.Protection)
		pkg, err := protector.Unprotect(context.Background(), credentialpackage.Protected{Envelope: got.Envelope, Body: gotBody}, got.Envelope.Audience[0])
		if err != nil {
			done <- outcome{err: err}
			return
		}
		proj, _ := pkg.SafeProjection()
		var rbuf bytes.Buffer
		_ = WriteReceipt(&rbuf, Receipt{PackageID: proj.PackageID, PackageDigest: proj.PackageDigest, GrantCount: len(pkg.Grants), BindingCount: len(pkg.Bindings), Accepted: true})
		_, _ = conn.Write(rbuf.Bytes())
		pkg.Destroy()
		done <- outcome{header: got, receipt: rbuf.Bytes()}
	}()
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(conn, h, body); err != nil {
		t.Fatal(err)
	}
	rc, err := ReadReceipt(conn)
	conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	res := <-done
	if res.err != nil {
		t.Fatal(res.err)
	}
	if !rc.Accepted || rc.PackageDigest != h.Envelope.PackageDigest {
		t.Fatalf("receipt %+v", rc)
	}
	if canary.Found(res.receipt, seed) {
		t.Fatal("the receipt carries the canary")
	}
	hj, _ := json.Marshal(res.header)
	if canary.Found(hj, seed) {
		t.Fatal("the header carries the canary")
	}
	// The header is a token container: it redacts under slog and fmt.
	text, _ := tok.MarshalText()
	canarytest.Exercise(t, res.header, canary.Capability, canary.Value{Raw: string(text)})
}
