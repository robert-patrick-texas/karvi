// Command release-fixture serves a releases page in GitHub's layout over
// HTTPS, with an HTTP and an HTTPS proxy before it, for the container test of
// karvi-sysutil package update (scripts/sysutil-smoke-test.sh). Each listener
// presents a self-signed certificate of its own for 127.0.0.1, written to the
// served folder as origin.pem and proxy.pem so that a test can trust one and
// not the other. It is a development tool and is not installed as a karvi
// operator executable.
//
// Under -dir, the file latest names the latest release's tag, and the folder
// of each tag holds its assets: /releases/latest redirects to
// /releases/tag/TAG, and /releases/download/TAG/NAME to /assets/TAG/NAME, as
// GitHub sends an asset to another host. Each request is logged on standard
// output, a proxy's tunnels with the listener's name.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	dir := flag.String("dir", "", "the served folder: latest, and a folder per tag")
	origin := flag.String("origin", "127.0.0.1:8443", "the releases' HTTPS address")
	proxy := flag.String("proxy", "127.0.0.1:8444", "the HTTP proxy's address")
	tlsProxy := flag.String("tls-proxy", "127.0.0.1:8445", "the HTTPS proxy's address")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "-dir is required")
		os.Exit(2)
	}
	originCert, err := certificate(filepath.Join(*dir, "origin.pem"))
	if err != nil {
		fatal(err)
	}
	proxyCert, err := certificate(filepath.Join(*dir, "proxy.pem"))
	if err != nil {
		fatal(err)
	}
	// No HTTP/2 on the proxies, whose CONNECT takes the connection over.
	noH2 := map[string]func(*http.Server, *tls.Conn, http.Handler){}
	errs := make(chan error, 3)
	go func() {
		s := &http.Server{Addr: *origin, Handler: releases(*dir), TLSConfig: &tls.Config{Certificates: []tls.Certificate{originCert}}}
		errs <- s.ListenAndServeTLS("", "")
	}()
	go func() {
		s := &http.Server{Addr: *proxy, Handler: tunnel("proxy"), TLSNextProto: noH2}
		errs <- s.ListenAndServe()
	}()
	go func() {
		s := &http.Server{Addr: *tlsProxy, Handler: tunnel("tls-proxy"), TLSNextProto: noH2,
			TLSConfig: &tls.Config{Certificates: []tls.Certificate{proxyCert}}}
		errs <- s.ListenAndServeTLS("", "")
	}()
	fatal(<-errs)
}

// releases serves the folder in GitHub's layout.
func releases(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("origin %s %s\n", r.Method, r.URL.Path)
		base := "https://" + r.Host
		switch path := r.URL.Path; {
		case path == "/releases/latest":
			tag, err := os.ReadFile(filepath.Join(dir, "latest"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			http.Redirect(w, r, base+"/releases/tag/"+strings.TrimSpace(string(tag)), http.StatusFound)
		case strings.HasPrefix(path, "/releases/tag/"):
			fmt.Fprintf(w, "release %s\n", strings.TrimPrefix(path, "/releases/tag/"))
		case strings.HasPrefix(path, "/releases/download/"):
			http.Redirect(w, r, base+"/assets/"+strings.TrimPrefix(path, "/releases/download/"), http.StatusFound)
		case strings.HasPrefix(path, "/assets/"):
			name := filepath.Join(dir, filepath.FromSlash(filepath.Clean("/"+strings.TrimPrefix(path, "/assets/"))))
			if info, err := os.Stat(name); err != nil || info.IsDir() {
				http.NotFound(w, r)
				return
			}
			http.ServeFile(w, r, name)
		default:
			http.NotFound(w, r)
		}
	})
}

// tunnel is a proxy that takes CONNECT alone, as curl sends for HTTPS.
func tunnel(name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("%s %s %s\n", name, r.Method, r.Host)
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT only", http.StatusMethodNotAllowed)
			return
		}
		up, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		conn, buf, err := http.NewResponseController(w).Hijack()
		if err != nil {
			up.Close()
			return
		}
		if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
			up.Close()
			conn.Close()
			return
		}
		go func() {
			_, _ = io.Copy(up, buf)
			up.Close()
		}()
		_, _ = io.Copy(conn, up)
		conn.Close()
	})
}

// certificate makes a self-signed certificate for 127.0.0.1 and writes it
// to path as PEM.
func certificate(path string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "release-fixture " + filepath.Base(path)},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
