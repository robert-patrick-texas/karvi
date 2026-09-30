// Package credentialframe is the local-peer credential channel: one
// length-prefixed binary frame per connection on the
// dedicated socket, then one receipt back. Nothing here parses an IPC
// envelope, and the envelope reader never opens this socket, so the
// exclusion from generic request logging is structural.
package credentialframe

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
)

// Wire layout, all integers big-endian:
//
//	u32 magic | u16 frame_version | u16 flags (0) | u32 header_len | u32 body_len | header | body
//
// The receipt is u32 length | JSON.
const (
	Magic        uint32 = 0x4E444350 // "NDCP"
	FrameVersion uint16 = 1
	MaxHeader           = 64 << 10
	MaxBody             = 4 << 20
	MaxReceipt          = 64 << 10
	prefixLen           = 16
)

// Limits bound what Read accepts; zero means the package constant.
type Limits struct {
	Header int
	Body   int
}

func (l Limits) effective() Limits {
	if l.Header <= 0 {
		l.Header = MaxHeader
	}
	if l.Body <= 0 {
		l.Body = MaxBody
	}
	return l
}

// Header is the frame's JSON header: the preparation the package answers,
// the one-use token, and the non-secret envelope.
type Header struct {
	PreparationID string                     `json:"preparation_id"`
	Token         ipc.ChannelToken           `json:"token"`
	Envelope      credentialpackage.Envelope `json:"envelope"`
}

// LogValue redacts the token; the envelope is safe.
func (h Header) LogValue() slog.Value {
	return slog.GroupValue(slog.String("preparation_id", h.PreparationID), slog.String("token", "<redacted>"), slog.String("package_id", h.Envelope.PackageID), slog.String("package_digest", h.Envelope.PackageDigest.String()))
}

// Receipt is the daemon's only reply.
type Receipt struct {
	PackageID     string                  `json:"package_id"`
	PackageDigest executionplan.Digest    `json:"package_digest"`
	GrantCount    int                     `json:"grant_count"`
	BindingCount  int                     `json:"binding_count"`
	Accepted      bool                    `json:"accepted"`
	Findings      []executionplan.Finding `json:"findings"`
}

func malformed(format string, args ...any) error {
	return errorcodes.Errorf("credential_frame_malformed", format, args...)
}

func tooLarge(what string, n, max int) error {
	return errorcodes.Errorf("credential_frame_too_large", "%s is %d bytes, maximum %d", what, n, max)
}

// Validate checks the header: a preparation ID, a token, and an envelope
// that validates and uses local-peer (the body carries the package).
func (h Header) Validate() error {
	if !executionplan.ValidID(h.PreparationID) {
		return malformed("preparation_id %q is not a well-formed identifier", h.PreparationID)
	}
	if h.Token.IsZero() {
		return malformed("token is required")
	}
	if err := h.Envelope.Validate(); err != nil {
		return err
	}
	if h.Envelope.Protection != credentialpackage.ProtectionLocalPeer {
		return errorcodes.Errorf("credential_package_invalid", "rule=protection_unsupported: the credential frame carries local-peer packages only")
	}
	return nil
}

// VerifyToken compares the header's token with the preparation's in
// constant time.
func (h Header) VerifyToken(expected ipc.ChannelToken) error {
	if !h.Token.Equal(expected) {
		return errorcodes.Errorf("credential_channel_token_invalid", "the credential frame's token does not match the preparation")
	}
	return nil
}

// Write sends one frame: the header and the encoded package body. The body
// is copied out of SecretBytes only for the write and wiped after.
func Write(w io.Writer, h Header, body credentials.SecretBytes) error {
	if err := h.Validate(); err != nil {
		return err
	}
	header, err := json.Marshal(h)
	if err != nil {
		return err
	}
	if len(header) > MaxHeader {
		return tooLarge("frame header", len(header), MaxHeader)
	}
	if body.Len() > MaxBody {
		return tooLarge("frame body", body.Len(), MaxBody)
	}
	if body.Len() == 0 {
		return malformed("frame body is empty")
	}
	prefix := make([]byte, prefixLen)
	binary.BigEndian.PutUint32(prefix[0:4], Magic)
	binary.BigEndian.PutUint16(prefix[4:6], FrameVersion)
	binary.BigEndian.PutUint16(prefix[6:8], 0)
	binary.BigEndian.PutUint32(prefix[8:12], uint32(len(header)))
	binary.BigEndian.PutUint32(prefix[12:16], uint32(body.Len()))
	if _, err := w.Write(prefix); err != nil {
		return err
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	return body.WithBytes(func(b []byte) error {
		_, err := w.Write(b)
		return err
	})
}

// Read receives one frame. Both lengths are checked against the limits
// before any allocation; a short read is malformed. The body is wrapped as
// SecretBytes and the read buffer wiped.
func Read(r io.Reader, limits Limits) (Header, credentials.SecretBytes, error) {
	limits = limits.effective()
	prefix := make([]byte, prefixLen)
	if _, err := io.ReadFull(r, prefix); err != nil {
		return Header{}, credentials.SecretBytes{}, malformed("frame prefix: %v", err)
	}
	if binary.BigEndian.Uint32(prefix[0:4]) != Magic {
		return Header{}, credentials.SecretBytes{}, malformed("bad magic %#x", binary.BigEndian.Uint32(prefix[0:4]))
	}
	if v := binary.BigEndian.Uint16(prefix[4:6]); v != FrameVersion {
		return Header{}, credentials.SecretBytes{}, malformed("unsupported frame version %d", v)
	}
	if f := binary.BigEndian.Uint16(prefix[6:8]); f != 0 {
		return Header{}, credentials.SecretBytes{}, malformed("reserved flags %#x are set", f)
	}
	headerLen := int(binary.BigEndian.Uint32(prefix[8:12]))
	bodyLen := int(binary.BigEndian.Uint32(prefix[12:16]))
	if headerLen > limits.Header {
		return Header{}, credentials.SecretBytes{}, tooLarge("frame header", headerLen, limits.Header)
	}
	if bodyLen > limits.Body {
		return Header{}, credentials.SecretBytes{}, tooLarge("frame body", bodyLen, limits.Body)
	}
	if headerLen == 0 || bodyLen == 0 {
		return Header{}, credentials.SecretBytes{}, malformed("frame header and body must be non-empty")
	}
	headerBytes := make([]byte, headerLen)
	if _, err := io.ReadFull(r, headerBytes); err != nil {
		return Header{}, credentials.SecretBytes{}, malformed("frame header: %v", err)
	}
	var h Header
	dec := json.NewDecoder(bytes.NewReader(headerBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&h); err != nil {
		return Header{}, credentials.SecretBytes{}, malformed("frame header: %v", err)
	}
	if err := h.Validate(); err != nil {
		return Header{}, credentials.SecretBytes{}, err
	}
	body := make([]byte, bodyLen)
	defer wipe(body)
	if _, err := io.ReadFull(r, body); err != nil {
		return Header{}, credentials.SecretBytes{}, malformed("frame body: %v", err)
	}
	return h, credentials.NewSecretBytes(body), nil
}

// WriteReceipt sends the length-prefixed receipt.
func WriteReceipt(w io.Writer, rc Receipt) error {
	if rc.Findings == nil {
		rc.Findings = []executionplan.Finding{}
	}
	data, err := json.Marshal(rc)
	if err != nil {
		return err
	}
	if len(data) > MaxReceipt {
		return tooLarge("receipt", len(data), MaxReceipt)
	}
	prefix := make([]byte, 4)
	binary.BigEndian.PutUint32(prefix, uint32(len(data)))
	if _, err := w.Write(prefix); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// ReadReceipt receives the receipt.
func ReadReceipt(r io.Reader) (Receipt, error) {
	prefix := make([]byte, 4)
	if _, err := io.ReadFull(r, prefix); err != nil {
		return Receipt{}, malformed("receipt prefix: %v", err)
	}
	n := int(binary.BigEndian.Uint32(prefix))
	if n > MaxReceipt {
		return Receipt{}, tooLarge("receipt", n, MaxReceipt)
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return Receipt{}, malformed("receipt: %v", err)
	}
	var rc Receipt
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rc); err != nil {
		return Receipt{}, malformed("receipt: %v", err)
	}
	return rc, nil
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
