// Package credcsv implements the credential CSV backend: a device-keyed
// credential file read with the shared tabular reader under the shared
// credential-file rules, whose rows are matched top to bottom, first match
// wins.
//
// The package owns little of its own. The file rules, the availability
// classes, and the read-once cache are credfile's; the parser is
// tabular.CSVReader; a row's selectors are compiled and matched by
// internal/matching. What is here is the load (every row checked once, at
// first use), the three credential_csv_* codes, the selected row's result,
// and its evidence and field sources.
//
// No message of this package quotes a cell. A misplaced delimiter can shift
// a password into any column, so an error names the backend, the file, the
// line, and the column, and never the text.
package credcsv

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/credfile"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/envindirect"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/matching"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
	"github.com/robert-patrick-texas/karvi/tabular"
)

// headerAliases are the headers a field answers to beside its own name and
// its hyphen spelling: device_name also reads the header name.
var headerAliases = map[string][]string{"device_name": {"name"}}

// Backend reads one credential CSV. The zero value is not usable; New
// builds one from a validated [credential-backend.NAME] table.
type Backend struct {
	Rules credfile.Rules
	Path  string

	// The reader keys. ReadMode is the configuration's mode
	// (header or numeric; Mode is the interface's backend mode). Aliases
	// serves header mode, Columns (one-based) numeric mode.
	ReadMode             string
	Delimiter            rune
	Aliases              map[string][]string
	Columns              map[string]int
	Mandatory            []string
	MaxPhysicalLineBytes int

	// Environment indirection, applied to the selected row only (decision
	// 6.5). LookupEnv is os.LookupEnv when nil.
	UsernameIndirect bool
	PasswordIndirect bool
	EnableIndirect   bool
	LookupEnv        func(string) (string, bool)

	cache credfile.Cache[*table]
}

// New builds the backend from its configuration table, which configload
// has validated (validateCredentialCSV): the types asserted here are the
// ones validation admits, and anything else is skipped rather than
// reported, since a table that failed validation never reaches a resolver.
func New(rules credfile.Rules, d map[string]any, maxPhysicalLineBytes int) *Backend {
	b := &Backend{Rules: rules, ReadMode: "header", Delimiter: ',', Aliases: map[string][]string{}, Columns: map[string]int{}, Mandatory: []string{"username"}, MaxPhysicalLineBytes: maxPhysicalLineBytes}
	b.Path, _ = d["path"].(string)
	if mode, _ := d["mode"].(string); mode != "" {
		b.ReadMode = mode
	}
	if s, _ := d["delimiter"].(string); s != "" {
		b.Delimiter = []rune(s)[0]
	}
	for _, field := range configschema.CredentialCSVFields() {
		// Mapped headers come first, then the field's own spellings. The
		// reader itself tries the field's own name before any alias, so a
		// file that holds both a mapped header and the field's own name
		// reads the field's own column, as an inventory file does.
		var mapped []string
		switch x := d["mappings."+field].(type) {
		case string:
			mapped = []string{x}
		case []any:
			for _, e := range x {
				if s, ok := e.(string); ok {
					mapped = append(mapped, s)
				}
			}
		case int64:
			b.Columns[field] = int(x)
		}
		b.Aliases[field] = append(append(mapped, field, strings.ReplaceAll(field, "_", "-")), headerAliases[field]...)
	}
	if list, ok := d["mandatory-fields"].([]any); ok && len(list) > 0 {
		b.Mandatory = nil
		for _, e := range list {
			if s, ok := e.(string); ok {
				b.Mandatory = append(b.Mandatory, s)
			}
		}
	}
	b.UsernameIndirect, _ = d["env-indirection.username"].(bool)
	b.PasswordIndirect, _ = d["env-indirection.password"].(bool)
	b.EnableIndirect, _ = d["env-indirection.enable-password"].(bool)
	return b
}

func (b *Backend) Name() string                { return b.Rules.BackendName }
func (*Backend) Mode() credentials.BackendMode { return credentials.DeviceKeyed }

// table is a loaded file: the compiled selectors and the results, index for
// index, in file order.
type table struct {
	path    string // the canonical path, for evidence and field sources
	matches []matching.Row
	rows    []row
}

// row is one data row's result. The secrets leave string form at load:
// a stray print of a row shows <redacted>.
type row struct {
	line     int
	label    string // the row's credkey, or BACKEND:LINE for a blank cell
	username string // trimmed; blank means the operator's own name
	password *secrets.Value
	enable   *secrets.Value
}

// load reads and checks the whole file once per backend and caches the
// table or the classified failure. Every row is checked
// here, not when a device happens to reach it, so a bad row fails every run
// the same way whatever targets were named.
func (b *Backend) load(ctx context.Context) (*table, *credentials.BackendResult) {
	path := b.Rules.ExpandPath(b.Path)
	return b.cache.Load(ctx,
		func() (*table, error) { return b.read(ctx, path) },
		func(err error) credentials.BackendResult {
			return b.Rules.Classify(path, err, "credential_csv_malformed")
		})
}

func (b *Backend) read(ctx context.Context, path string) (*table, error) {
	// Open first, then check what was opened; the descriptor
	// checked is the one the reader reads. The permission rules run before
	// a byte is parsed.
	f, canonical, err := b.Rules.Open(path)
	if err != nil {
		if credfile.IsUnavailable(err) {
			return nil, &credfile.UnavailableError{Err: err}
		}
		return nil, err
	}
	defer f.Close()
	// The reader's tabular_* codes are inventory's (category inventory,
	// exit 5). A credential file that fails to parse is a credential error,
	// so every reader failure is reported under credential_csv_malformed
	// with the reader's own reason kept.
	malformed := func(err error) error {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		// The reason is the reader's text without its tabular_* code: one
		// message, one code, and a search for the code finds this cause.
		reason := err.Error()
		if code := errorcodes.Of(err); code != "" {
			reason = strings.TrimPrefix(reason, code+": ")
		}
		// The reader quotes a duplicated header. In a file with no header
		// row read under header mode the "header" is the first credential
		// row, and a row whose username and password are equal would put
		// the password in the message; this one reason is restated.
		if errorcodes.Of(err) == "tabular_header_duplicate" {
			reason = "a header appears twice (a file with no header row needs mode = \"numeric\")"
		}
		return errorcodes.Errorf("credential_csv_malformed", "credential backend %s: %s: %s", b.Rules.BackendName, canonical, reason)
	}
	fields := configschema.CredentialCSVFields()
	// Trim is off so a secret keeps its spaces; the selector, key, and
	// username cells are trimmed below. A short row is an error: in a
	// first-match file a skipped row changes which row wins.
	it, err := (tabular.CSVReader{}).Read(ctx, f, tabular.Spec{Delimiter: b.Delimiter, Mode: b.ReadMode, CommentPrefix: '#', Requested: fields, Aliases: b.Aliases, Mandatory: b.Mandatory, NumericMappings: b.Columns, Trim: false, MaxPhysicalLineBytes: b.MaxPhysicalLineBytes, ShortRowPolicy: "error"})
	if err != nil {
		return nil, malformed(err)
	}
	t := &table{path: canonical}
	keys := map[string]int{} // folded key -> line
	for it.Next() {
		rec := it.Record()
		invalid := func(reason string) error {
			return errorcodes.Errorf("credential_csv_row_invalid", "credential backend %s: %s:%d: %s", b.Rules.BackendName, canonical, rec.Line, reason)
		}
		// CompileRow reads the six selector columns, trims them, and
		// refuses a row with no positive selector; its
		// error names the column and never the cell.
		match, err := matching.CompileRow(rec.Values)
		if err != nil {
			return nil, invalid(err.Error())
		}
		r := row{line: rec.Line, username: strings.TrimSpace(rec.Values["username"]), password: secretCell(rec.Values["password"]), enable: secretCell(rec.Values["enable_password"])}
		if r.username == "" && r.password == nil && r.enable == nil {
			return nil, invalid("the row has no result: username, password, and enable_password are all blank")
		}
		r.label = match.Key()
		if r.label == "" {
			r.label = fmt.Sprintf("%s:%d", b.Rules.BackendName, rec.Line)
		} else {
			// A filled key is unique in the file, compared as it is
			// matched, case folded. The message
			// names both lines and not the key.
			folded := matching.FoldKey(match.Key())
			if first, seen := keys[folded]; seen {
				return nil, errorcodes.Errorf("credential_csv_credkey_duplicate", "credential backend %s: %s: lines %d and %d have the same credkey", b.Rules.BackendName, canonical, first, rec.Line)
			}
			keys[folded] = rec.Line
		}
		t.matches = append(t.matches, match)
		t.rows = append(t.rows, r)
	}
	if err := it.Err(); err != nil {
		return nil, malformed(err)
	}
	return t, nil
}

// secretCell keeps a secret cell verbatim, outside string form. A cell of
// nothing but white space is blank: a password of spaces alone is not a
// password anyone sets, and a spreadsheet leaves such cells behind.
func secretCell(cell string) *secrets.Value {
	if strings.TrimSpace(cell) == "" {
		return nil
	}
	return secrets.New(cell)
}

// HonoursCredKey declares the keyed capability (credentials.Keyed; decision
// 4.9): for a pinned device Resolve answers with the row holding the key or
// with NotFound, never with a general row, so the resolver asks this
// backend while it walks a pinned device's sequence.
func (b *Backend) HonoursCredKey() bool { return true }

// Resolve selects the first row that matches the device by its selectors
// alone; that row is the answer, and whether it is complete is the
// resolver's question afterwards. An incomplete row never
// falls through to a later row.
func (b *Backend) Resolve(ctx context.Context, req credentials.ResolveRequest) credentials.BackendResult {
	t, failed := b.load(ctx)
	if failed != nil {
		return *failed
	}
	d := req.Device
	// The device view is the one the policy map is matched against, and
	// CredKeyRef, the inventory's credkeyref, is its sixth field. A pinned
	// device matches only the row whose key equals its reference, that
	// row's other filled cells still applying; Row.Match implements it.
	// Blank means unpinned, for which the key column is ignored and a row
	// needs a positive cell other than its key.
	view := matching.RowFields{Fields: matching.Fields{Name: d.CanonicalName, Address: d.ManagementAddress, Platform: d.Platform, Site: d.Site, Groups: d.Groups}, CredKeyRef: d.CredKeyRef}
	i := matching.FirstRow(t.matches, view)
	if i < 0 {
		return credentials.BackendResult{Outcome: credentials.NotFound}
	}
	r := t.rows[i]
	at := func(column string) string { return fmt.Sprintf("%s:%d %s", t.path, r.line, column) }
	sources := map[string]credentials.FieldSource{}

	// The username: the cell, or the operator's own name for a blank cell,
	// as .cloginrc does when no user line matches. The
	// request's operator name has the backend's operator transform applied.
	username, usource := r.username, at("username")
	if username == "" {
		username, usource = req.Operator.Username, "operator-default"
	} else if v, name, ok := envindirect.Lookup(username, b.UsernameIndirect, b.LookupEnv); ok {
		username, usource = v, envindirect.Source(name)
	}
	sources["username"] = credentials.FieldSource{Backend: b.Rules.BackendName, Path: usource}

	// The material is a copy of the row's stored values:
	// destroying it after the grant is sealed leaves the cached row whole
	// for the next device.
	material := &secrets.Material{Username: secrets.New(username)}
	var err error
	if material.Password, err = b.secret(r.password, b.PasswordIndirect, "password", at, sources); err == nil {
		material.EnablePassword, err = b.secret(r.enable, b.EnableIndirect, "enable_password", at, sources)
	}
	if err != nil {
		material.Destroy()
		return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: "credential_material_error", Message: fmt.Sprintf("credential backend %s: %v", b.Rules.BackendName, err)}
	}
	// The evidence says what matched: the row's filled selectors as
	// written, the file, the line, and the row's key or generated label.
	// It never names the device, so devices that take one row share one
	// grant.
	cred := credentials.Credential{Material: material, Backend: b.Rules.BackendName, Policy: req.Policy, MatchedOn: credentials.Match{Category: "csv_row", Pattern: t.matches[i].String(), Source: t.path, Line: r.line, CredKey: r.label}, FieldSources: sources}
	return credentials.BackendResult{Outcome: credentials.Success, Credential: cred}
}

// secret copies one stored secret into the answer and records its field
// source. A blank cell gives an unset value and no source. With the field's
// indirection flag on, a cell that names a set environment variable is
// replaced by the variable's value; the name is looked up
// without the cell's surrounding white space, and a cell that names no set
// variable stays the literal it is.
func (b *Backend) secret(stored *secrets.Value, indirect bool, field string, at func(string) string, sources map[string]credentials.FieldSource) (*secrets.Value, error) {
	if stored == nil {
		return secrets.New(""), nil
	}
	var out *secrets.Value
	source := at(field)
	err := stored.WithBytes(func(cell []byte) error {
		if indirect {
			if v, name, ok := envindirect.Lookup(strings.TrimSpace(string(cell)), true, b.LookupEnv); ok {
				out, source = secrets.New(v), envindirect.Source(name)
				return nil
			}
		}
		out = secrets.NewBytes(cell)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sources[field] = credentials.FieldSource{Backend: b.Rules.BackendName, Path: source}
	return out, nil
}
