// Package vault implements a minimal Vault KV-v2 credential backend using the
// standard HTTP and TLS libraries. It performs one read and no retries.
package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/envindirect"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/storetls"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
)

// Backend is built once by the resolver, every default set there, and
// Resolve reads it and never writes it, so devices resolving in parallel
// share it.
type Backend struct {
	BackendName, Address, Namespace, Mount, PathTemplate, TokenEnv, CAFile, ClientCertFile, ClientKeyFile, UsernameField, PasswordField, EnableField string
	TLSVerify                                                                                                                                        bool
	Timeout                                                                                                                                          time.Duration
}

func (b *Backend) Name() string                { return b.BackendName }
func (*Backend) Mode() credentials.BackendMode { return credentials.OperatorKeyed }

// Resolve reads the operator's record at the path-template with %operator%
// filled. The token, the variable token-env names, is read through the
// request's lookup before any connection, an unset or empty one
// credential_env_missing. A record's field env:NAME is the value of the
// variable NAME, read through the same lookup. The credential's source is
// the path read.
func (b *Backend) Resolve(ctx context.Context, req credentials.ResolveRequest) credentials.BackendResult {
	token, missing := envindirect.Read(b.TokenEnv, req.LookupEnv)
	if missing != "" {
		return envindirect.Missing(b.BackendName, "token-env", missing)
	}
	// The load refuses an address that is not an http:// or https:// URL.
	base, err := url.Parse(strings.TrimRight(b.Address, "/"))
	if err != nil {
		return malformed("vault_request", err)
	}
	base.Path = path.Join(base.Path, "v1", b.Mount, "data", req.Fill(b.PathTemplate))
	client, err := b.client()
	if err != nil {
		code := "vault_tls"
		if errors.Is(err, storetls.ErrNoCertificates) {
			code = "vault_ca_file_empty"
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
	values, found, failed := envindirect.Record(b.BackendName, base.Path, "vault_response_malformed", [3]string{b.UsernameField, b.PasswordField, b.EnableField}, func(f string) (string, bool) { return stringField(envelope.Data.Data, f) }, req.LookupEnv)
	if failed != nil {
		return *failed
	}
	if !found {
		return credentials.BackendResult{Outcome: credentials.NotFound}
	}
	cred := credentials.Credential{Material: secrets.NewMaterial(values[0], values[1], values[2]), Backend: b.BackendName, Policy: req.Policy, MatchedOn: credentials.Match{Category: "operator", SafeValue: req.Operator.Username, Source: base.Path}}
	return credentials.BackendResult{Outcome: credentials.Success, Credential: cred}
}
func (b *Backend) client() (*http.Client, error) {
	tlsCfg, err := storetls.Config(b.CAFile, b.ClientCertFile, b.ClientKeyFile, b.TLSVerify)
	if err != nil {
		return nil, err
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
