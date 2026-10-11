package storetls

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/storetls/tlstest"
)

// TestConfig is EXAMPLES chapter 56, issue 16: a named CA file is the whole
// trust, not added to the host's; without one the host's trust (RootCAs
// nil); tls-verify = false checks nothing; a client file's further
// certificates are its chain; TLS 1.2 at least.
func TestConfig(t *testing.T) {
	site := tlstest.New(t, "site CA")
	cfg, err := Config(site.CA, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RootCAs == nil || !cfg.RootCAs.Equal(site.Pool) || cfg.InsecureSkipVerify || cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("a CA file: the pool is not the file's alone, or verification is off")
	}
	if system, err := x509.SystemCertPool(); err == nil && cfg.RootCAs.Equal(system) {
		t.Errorf("a CA file: the host's trust")
	}
	cfg, err = Config("", "", "", true)
	if err != nil || cfg.RootCAs != nil || cfg.InsecureSkipVerify {
		t.Errorf("no CA file: %+v %v, want the host's trust", cfg, err)
	}
	cfg, err = Config("", "", "", false)
	if err != nil || !cfg.InsecureSkipVerify {
		t.Errorf("verify off: %v", err)
	}
	cfg, err = Config("", site.ClientCert, site.ClientKey, true)
	if err != nil || len(cfg.Certificates) != 1 || len(cfg.Certificates[0].Certificate) != 2 {
		t.Errorf("the client pair: %v, want one certificate with a chain of two", err)
	}
	empty := filepath.Join(t.TempDir(), "empty.pem")
	if err := os.WriteFile(empty, []byte("no certificate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Config(empty, "", "", true); !errors.Is(err, ErrNoCertificates) {
		t.Errorf("an empty CA file: %v", err)
	}
	if _, err := Config(filepath.Join(t.TempDir(), "absent.pem"), "", "", true); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an absent CA file: %v", err)
	}
}
