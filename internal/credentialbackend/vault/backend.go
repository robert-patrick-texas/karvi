// Package vault implements a minimal Vault KV-v2 credential backend using the
// standard HTTP and TLS libraries. It performs one read and no retries.
package vault

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
)

type Backend struct {
	BackendName, Address, Namespace, Mount, PathTemplate, TokenEnv, CAFile, ClientCertFile, ClientKeyFile, UsernameField, PasswordField, EnableField string
	Timeout                                                                                                                                          time.Duration
	UsernameIndirect, PasswordIndirect, EnableIndirect                                                                                               bool
}

func (b *Backend) Name() string                { return b.BackendName }
func (*Backend) Mode() credentials.BackendMode { return credentials.OperatorKeyed }
func (b *Backend) Resolve(ctx context.Context, req credentials.ResolveRequest) credentials.BackendResult {
	if b.TokenEnv == "" {
		b.TokenEnv = "VAULT_TOKEN"
	}
	token, ok := os.LookupEnv(b.TokenEnv)
	if !ok || token == "" {
		return credentials.BackendResult{Outcome: credentials.PermissionDenied, ErrorCode: "vault_token_missing", Message: "Vault token environment variable is absent"}
	}
	if b.Mount == "" {
		b.Mount = "secret"
	}
	if b.PathTemplate == "" {
		b.PathTemplate = "karvi/%s"
	}
	if b.UsernameField == "" {
		b.UsernameField = "username"
	}
	if b.PasswordField == "" {
		b.PasswordField = "password"
	}
	if b.EnableField == "" {
		b.EnableField = "enable_password"
	}
	if b.Timeout <= 0 {
		b.Timeout = 5 * time.Second
	}
	base, err := url.Parse(strings.TrimRight(b.Address, "/"))
	if err != nil {
		return malformed("vault_address", err)
	}
	base.Path = path.Join(base.Path, "v1", b.Mount, "data", fmt.Sprintf(b.PathTemplate, req.Operator.Username))
	client, err := b.client()
	if err != nil {
		code := errorcodes.Of(err)
		if code == "" {
			code = "vault_tls"
		}
		return malformed(code, err)
	}
	qctx, cancel := context.WithTimeout(ctx, b.Timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(qctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return malformed("vault_request", err)
	}
	httpReq.Header.Set("X-Vault-Token", token)
	if b.Namespace != "" {
		httpReq.Header.Set("X-Vault-Namespace", b.Namespace)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return credentials.BackendResult{Outcome: credentials.Unavailable, ErrorCode: "vault_unavailable", Message: safeErr(err), Retryable: isTimeout(err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return malformed("vault_read", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return credentials.BackendResult{Outcome: credentials.NotFound}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return credentials.BackendResult{Outcome: credentials.Unavailable, ErrorCode: "vault_http_error", Message: fmt.Sprintf("Vault returned HTTP %d", resp.StatusCode)}
	}
	var envelope struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return malformed("vault_response_malformed", err)
	}
	username, uok := stringField(envelope.Data.Data, b.UsernameField)
	password, pok := stringField(envelope.Data.Data, b.PasswordField)
	enable, eok := stringField(envelope.Data.Data, b.EnableField)
	if !uok && !pok && !eok {
		return credentials.BackendResult{Outcome: credentials.NotFound}
	}
	username, usrc := indirect(username, b.UsernameIndirect, "vault:"+b.UsernameField)
	password, psrc := indirect(password, b.PasswordIndirect, "vault:"+b.PasswordField)
	enable, esrc := indirect(enable, b.EnableIndirect, "vault:"+b.EnableField)
	cred := credentials.Credential{Material: secrets.NewMaterial(username, password, enable), Backend: b.BackendName, Policy: req.Policy, MatchedOn: credentials.Match{Category: "operator", SafeValue: req.Operator.Username, Source: base.Path}, FieldSources: map[string]credentials.FieldSource{"username": {Backend: b.BackendName, Path: usrc}, "password": {Backend: b.BackendName, Path: psrc}, "enable_password": {Backend: b.BackendName, Path: esrc}}}
	return credentials.BackendResult{Outcome: credentials.Success, Credential: cred}
}
func (b *Backend) client() (*http.Client, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if b.CAFile != "" {
		pem, err := os.ReadFile(b.CAFile)
		if err != nil {
			return nil, err
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errorcodes.Errorf("vault_ca_file_empty", "CA file contains no certificates")
		}
		tlsCfg.RootCAs = pool
	}
	if b.ClientCertFile != "" || b.ClientKeyFile != "" {
		if b.ClientCertFile == "" || b.ClientKeyFile == "" {
			return nil, errorcodes.Errorf("vault_client_cert_key_unpaired", "client certificate and key must be configured together")
		}
		cert, err := tls.LoadX509KeyPair(b.ClientCertFile, b.ClientKeyFile)
		if err != nil {
			return nil, err
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg}, Timeout: b.Timeout}, nil
}
func malformed(code string, err error) credentials.BackendResult {
	return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: code, Message: safeErr(err)}
}
func safeErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func isTimeout(err error) bool {
	type t interface{ Timeout() bool }
	x, ok := err.(t)
	return ok && x.Timeout()
}
func stringField(m map[string]any, k string) (string, bool) {
	v, ok := m[k]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
func indirect(v string, on bool, src string) (string, string) {
	if !on {
		return v, src
	}
	if x, ok := os.LookupEnv(v); ok {
		return x, "env:" + v
	}
	return v, "literal"
}
