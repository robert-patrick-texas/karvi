// Package tlstest makes the certificates the store backends' tests use: a
// CA, a server certificate for 127.0.0.1 it signs, and a client certificate
// it signs, written as PEM files in a test's directory.
package tlstest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Set is one CA's files and the server's certificate. Client holds the
// client's certificate followed by the CA's, a chain of two.
type Set struct {
	CA, ClientCert, ClientKey string
	Server                    tls.Certificate
	// Pool trusts the CA alone, for a test server that verifies clients.
	Pool *x509.CertPool
}

// New makes a CA named name and its two certificates under a new temporary
// directory.
func New(t testing.TB, name string) Set {
	t.Helper()
	dir := t.TempDir()
	caKey := key(t)
	caTmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER := sign(t, caTmpl, caTmpl, caKey, caKey)
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leaf := func(serial int64, cn string, usage x509.ExtKeyUsage) ([]byte, *ecdsa.PrivateKey) {
		k := key(t)
		tmpl := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: cn}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
		if usage == x509.ExtKeyUsageServerAuth {
			tmpl.IPAddresses = []net.IP{net.IPv4(127, 0, 0, 1)}
		}
		return sign(t, tmpl, caCert, k, caKey), k
	}
	serverDER, serverKey := leaf(2, "127.0.0.1", x509.ExtKeyUsageServerAuth)
	clientDER, clientKey := leaf(3, "karvi", x509.ExtKeyUsageClientAuth)
	s := Set{CA: filepath.Join(dir, "ca.pem"), ClientCert: filepath.Join(dir, "client.pem"), ClientKey: filepath.Join(dir, "client.key"), Pool: x509.NewCertPool()}
	s.Pool.AddCert(caCert)
	write(t, s.CA, block("CERTIFICATE", caDER))
	write(t, s.ClientCert, append(block("CERTIFICATE", clientDER), block("CERTIFICATE", caDER)...))
	write(t, s.ClientKey, keyBlock(t, clientKey))
	if s.Server, err = tls.X509KeyPair(block("CERTIFICATE", serverDER), keyBlock(t, serverKey)); err != nil {
		t.Fatal(err)
	}
	return s
}

func key(t testing.TB) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func sign(t testing.TB, tmpl, parent *x509.Certificate, k, parentKey *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &k.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func block(typ string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
}

func keyBlock(t testing.TB, k *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return block("EC PRIVATE KEY", der)
}

func write(t testing.TB, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
