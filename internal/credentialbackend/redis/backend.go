// Package redis implements the bounded subset of RESP needed for an
// operator-keyed HGETALL credential record. It uses one connection and no retry.
package redis

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/envindirect"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/storetls"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
)

// Backend is built once by the resolver, every default set there, and
// Resolve reads it and never writes it, so devices resolving in parallel
// share it.
type Backend struct {
	BackendName, Address, KeyTemplate, CAFile, ClientCertFile, ClientKeyFile, AuthUsernameEnv, AuthPasswordEnv string
	Database                                                                                                   int
	TLS, TLSVerify                                                                                             bool
	Timeout                                                                                                    time.Duration
}

func (b *Backend) Name() string                { return b.BackendName }
func (*Backend) Mode() credentials.BackendMode { return credentials.OperatorKeyed }

// Resolve reads the operator's record, the hash at the key-template with
// %operator% filled. The login's variables, auth-password-env and beside it
// auth-username-env, are read through the request's lookup before any
// connection, an unset or empty one credential_env_missing; "" names none,
// and no AUTH is sent. A record's field env:NAME is the value of the
// variable NAME, read through the same lookup. The credential's source is
// the key read.
func (b *Backend) Resolve(ctx context.Context, req credentials.ResolveRequest) credentials.BackendResult {
	var auth []string
	if b.AuthPasswordEnv != "" {
		pass, missing := envindirect.Read(b.AuthPasswordEnv, req.LookupEnv)
		if missing != "" {
			return envindirect.Missing(b.BackendName, "auth-password-env", missing)
		}
		auth = []string{"AUTH", pass}
		if b.AuthUsernameEnv != "" {
			user, missing := envindirect.Read(b.AuthUsernameEnv, req.LookupEnv)
			if missing != "" {
				return envindirect.Missing(b.BackendName, "auth-username-env", missing)
			}
			auth = []string{"AUTH", user, pass}
		}
	}
	dialer := net.Dialer{Timeout: b.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", b.Address)
	if err != nil {
		return unavailable(err)
	}
	if b.TLS {
		tlsCfg, err := storetls.Config(b.CAFile, b.ClientCertFile, b.ClientKeyFile, b.TLSVerify)
		if err != nil {
			conn.Close()
			code := "redis_tls_config_invalid"
			if errors.Is(err, storetls.ErrNoCertificates) {
				code = "redis_ca_file_empty"
			}
			return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: code, Message: err.Error()}
		}
		host, _, _ := net.SplitHostPort(b.Address)
		tlsCfg.ServerName = host
		tc := tls.Client(conn, tlsCfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			conn.Close()
			return unavailable(err)
		}
		conn = tc
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(b.Timeout))
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	if auth != nil {
		if _, err := command(rw, auth...); err != nil {
			return credentials.BackendResult{Outcome: credentials.PermissionDenied, ErrorCode: "redis_auth_failed", Message: "Redis authentication failed"}
		}
	}
	if b.Database != 0 {
		if _, err := command(rw, "SELECT", strconv.Itoa(b.Database)); err != nil {
			return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: "redis_select_failed", Message: err.Error()}
		}
	}
	key := req.Fill(b.KeyTemplate)
	reply, err := command(rw, "HGETALL", key)
	if err != nil {
		if code := errorcodes.Of(err); code != "" {
			return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: code, Message: err.Error()}
		}
		return unavailable(err)
	}
	arr, ok := reply.([]any)
	if !ok {
		return malformed(fmt.Errorf("HGETALL did not return array"))
	}
	if len(arr) == 0 {
		return credentials.BackendResult{Outcome: credentials.NotFound}
	}
	if len(arr)%2 != 0 {
		return malformed(fmt.Errorf("HGETALL returned odd field count"))
	}
	fields := map[string]string{}
	for i := 0; i < len(arr); i += 2 {
		k, kok := arr[i].(string)
		v, vok := arr[i+1].(string)
		if !kok || !vok {
			return malformed(fmt.Errorf("HGETALL field is not bulk string"))
		}
		fields[k] = v
	}
	values, found, failed := envindirect.Record(b.BackendName, key, "redis_malformed", [3]string{"username", "password", "enable_password"}, func(f string) (string, bool) { v, ok := fields[f]; return v, ok }, req.LookupEnv)
	if failed != nil {
		return *failed
	}
	if !found {
		return credentials.BackendResult{Outcome: credentials.NotFound}
	}
	cred := credentials.Credential{Material: secrets.NewMaterial(values[0], values[1], values[2]), Backend: b.BackendName, Policy: req.Policy, MatchedOn: credentials.Match{Category: "operator", SafeValue: req.Operator.Username, Source: key}}
	return credentials.BackendResult{Outcome: credentials.Success, Credential: cred}
}

func command(rw *bufio.ReadWriter, args ...string) (any, error) {
	fmt.Fprintf(rw, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(rw, "$%d\r\n%s\r\n", len(a), a)
	}
	if err := rw.Flush(); err != nil {
		return nil, err
	}
	return readReply(rw.Reader)
}
func readReply(r *bufio.Reader) (any, error) {
	prefix, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	switch prefix {
	case '+':
		return line, nil
	case '-':
		return nil, errorcodes.Errorf("redis_command_rejected", "redis error: %s", line)
	case ':':
		return strconv.ParseInt(line, 10, 64)
	case '$':
		n, err := strconv.Atoi(line)
		if err != nil {
			return nil, errorcodes.Errorf("redis_protocol_error", "invalid RESP length %q: %w", line, err)
		}
		if n < 0 {
			return nil, nil
		}
		if n > 2<<20 {
			return nil, errorcodes.Errorf("redis_protocol_error", "redis bulk reply too large")
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	case '*':
		n, err := strconv.Atoi(line)
		if err != nil {
			return nil, errorcodes.Errorf("redis_protocol_error", "invalid RESP length %q: %w", line, err)
		}
		if n < 0 {
			return nil, nil
		}
		if n > 10000 {
			return nil, errorcodes.Errorf("redis_protocol_error", "redis array too large")
		}
		out := make([]any, n)
		for i := range out {
			out[i], err = readReply(r)
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	return nil, errorcodes.Errorf("redis_protocol_error", "unknown RESP prefix %q", prefix)
}
func readLine(r *bufio.Reader) (string, error) {
	s, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(s, "\r\n") {
		return "", errorcodes.Errorf("redis_protocol_error", "invalid RESP line")
	}
	return strings.TrimSuffix(s, "\r\n"), nil
}
func unavailable(err error) credentials.BackendResult {
	return credentials.BackendResult{Outcome: credentials.Unavailable, ErrorCode: "redis_unavailable", Message: err.Error(), Retryable: true}
}
func malformed(err error) credentials.BackendResult {
	return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: "redis_malformed", Message: err.Error()}
}
