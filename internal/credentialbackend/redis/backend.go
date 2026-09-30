// Package redis implements the bounded subset of RESP needed for an
// operator-keyed HGETALL credential record. It uses one connection and no retry.
package redis

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
)

type Backend struct {
	BackendName, Address, KeyTemplate, CAFile, ClientCertFile, ClientKeyFile, AuthUsernameEnv, AuthPasswordEnv string
	Database                                                                                                   int
	TLS                                                                                                        bool
	Timeout                                                                                                    time.Duration
	UsernameIndirect, PasswordIndirect, EnableIndirect                                                         bool
}

func (b *Backend) Name() string                { return b.BackendName }
func (*Backend) Mode() credentials.BackendMode { return credentials.OperatorKeyed }
func (b *Backend) Resolve(ctx context.Context, req credentials.ResolveRequest) credentials.BackendResult {
	if b.Address == "" {
		return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: "redis_address_missing", Message: "Redis address is required"}
	}
	if b.KeyTemplate == "" {
		b.KeyTemplate = "karvi:credential:%s"
	}
	if b.Timeout <= 0 {
		b.Timeout = 2 * time.Second
	}
	dialer := net.Dialer{Timeout: b.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", b.Address)
	if err != nil {
		return unavailable(err)
	}
	if b.TLS {
		tlsCfg, err := b.tlsConfig()
		if err != nil {
			conn.Close()
			code := errorcodes.Of(err)
			if code == "" {
				code = "redis_tls_config_invalid"
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
	if b.AuthPasswordEnv != "" {
		pass, ok := os.LookupEnv(b.AuthPasswordEnv)
		if !ok {
			return credentials.BackendResult{Outcome: credentials.PermissionDenied, ErrorCode: "redis_auth_missing", Message: "Redis authentication environment variable is absent"}
		}
		args := []string{"AUTH", pass}
		if b.AuthUsernameEnv != "" {
			user, _ := os.LookupEnv(b.AuthUsernameEnv)
			args = []string{"AUTH", user, pass}
		}
		if _, err := command(rw, args...); err != nil {
			return credentials.BackendResult{Outcome: credentials.PermissionDenied, ErrorCode: "redis_auth_failed", Message: "Redis authentication failed"}
		}
	}
	if b.Database != 0 {
		if _, err := command(rw, "SELECT", strconv.Itoa(b.Database)); err != nil {
			return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: "redis_select_failed", Message: err.Error()}
		}
	}
	key := fmt.Sprintf(b.KeyTemplate, req.Operator.Username)
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
	username, uok := fields["username"]
	password, pok := fields["password"]
	enable, eok := fields["enable_password"]
	if !uok && !pok && !eok {
		return credentials.BackendResult{Outcome: credentials.NotFound}
	}
	username, usrc := indirect(username, b.UsernameIndirect, "redis:username")
	password, psrc := indirect(password, b.PasswordIndirect, "redis:password")
	enable, esrc := indirect(enable, b.EnableIndirect, "redis:enable_password")
	cred := credentials.Credential{Material: secrets.NewMaterial(username, password, enable), Backend: b.BackendName, Policy: req.Policy, MatchedOn: credentials.Match{Category: "operator", SafeValue: req.Operator.Username, Source: "redis-key-digest-not-implemented"}, FieldSources: map[string]credentials.FieldSource{"username": {Backend: b.BackendName, Path: usrc}, "password": {Backend: b.BackendName, Path: psrc}, "enable_password": {Backend: b.BackendName, Path: esrc}}}
	return credentials.BackendResult{Outcome: credentials.Success, Credential: cred}
}
func (b *Backend) tlsConfig() (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
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
			return nil, errorcodes.Errorf("redis_ca_file_empty", "CA file contains no certificates")
		}
		cfg.RootCAs = pool
	}
	if b.ClientCertFile != "" || b.ClientKeyFile != "" {
		if b.ClientCertFile == "" || b.ClientKeyFile == "" {
			return nil, errorcodes.Errorf("redis_client_cert_key_unpaired", "client certificate and key must be paired")
		}
		cert, err := tls.LoadX509KeyPair(b.ClientCertFile, b.ClientKeyFile)
		if err != nil {
			return nil, err
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
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
func indirect(v string, on bool, src string) (string, string) {
	if !on {
		return v, src
	}
	if x, ok := os.LookupEnv(v); ok {
		return x, "env:" + v
	}
	return v, "literal"
}
