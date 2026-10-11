// Package storetls is the TLS a Redis or Vault credential backend speaks to
// its store: TLS 1.2 at least; under verification the server's certificate
// chain and its name checked, the chain leading to a CA in the backend's CA
// file when one is named, the file then the backend's whole trust, or to the
// host's trust when none is; without verification neither checked, the
// connection still encrypted. A client certificate pair is presented with
// every further certificate in its file as the chain, and neither file's
// permissions are checked: one certificate may serve a site's machine
// authentication, and restricting the files is the site's choice.
package storetls

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
)

// ErrNoCertificates is a CA file that holds no certificate.
var ErrNoCertificates = errors.New("CA file contains no certificates")

// Config is the client configuration for a store: caFile, certFile, and
// keyFile are the backend's resolved files, empty for none (the load refuses
// a certificate without its key, the reverse, and a CA file under verify =
// false); verify is the backend's tls-verify. The server's name is the
// caller's to set.
func Config(caFile, certFile, keyFile string, verify bool) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: !verify}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, ErrNoCertificates
		}
		cfg.RootCAs = pool
	}
	if certFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, err
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}
