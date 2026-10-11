package transform

import (
	"sort"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// The configuration's named transform tables, built in one place for config
// validate and for every reader at the run, so a table the load passed
// builds the same at the run: [name-transform.NAME], whose operations
// rewrite a device's name, and [credential-transform.NAME], whose operator
// list rewrites the operator's name before a backend is asked and whose
// username list rewrites the username a backend answers. Each takes the
// tables as Snapshot.NamedTables gives them, and a "default" no table
// defines changes nothing.

// TableError is a table's list that does not build. Key is the list's
// configuration path (name-transform.lab.operations), which config validate
// reports with the list's place; Err carries the code.
type TableError struct {
	Key string
	Err error
}

func (e *TableError) Error() string { return e.Key + ": " + e.Err.Error() }
func (e *TableError) Unwrap() error { return e.Err }

// Credential is one credential transform table.
type Credential struct{ Operator, Username Sequence }

// kind is one sort of table: its prefix, and its errors for a list that is
// not an array and for an operation (from 1) that is not an inline table.
type kind struct {
	prefix   string
	notArray func() error
	notTable func(i int) error
}

var (
	nameKind = kind{
		prefix: "name-transform",
		notArray: func() error {
			return errorcodes.Errorf("name_transform_operations_not_array", "the operations are not an array of inline tables")
		},
		notTable: func(i int) error {
			return errorcodes.Errorf("name_transform_operation_not_table", "operation %d is not an inline table", i)
		},
	}
	credentialKind = kind{
		prefix: "credential-transform",
		notArray: func() error {
			return errorcodes.Errorf("credential_transform_operations_not_array", "the operations are not an array of inline tables")
		},
		notTable: func(i int) error {
			return errorcodes.Errorf("credential_transform_operation_not_table", "operation %d is not an inline table", i)
		},
	}
)

// Names builds every name transform table, by name.
func Names(tables map[string]map[string]any) (map[string]Sequence, error) {
	out := map[string]Sequence{"default": {}}
	for _, name := range sortedNames(tables) {
		seq, err := nameKind.list(name, "operations", tables[name])
		if err != nil {
			return nil, err
		}
		out[name] = seq
	}
	return out, nil
}

// Credentials builds every credential transform table, by name.
func Credentials(tables map[string]map[string]any) (map[string]Credential, error) {
	out := map[string]Credential{"default": {}}
	for _, name := range sortedNames(tables) {
		operator, err := credentialKind.list(name, "operator", tables[name])
		if err != nil {
			return nil, err
		}
		username, err := credentialKind.list(name, "username", tables[name])
		if err != nil {
			return nil, err
		}
		out[name] = Credential{Operator: operator, Username: username}
	}
	return out, nil
}

// list builds one table's list of operations; a list the table leaves out
// is no operation.
func (k kind) list(name, field string, table map[string]any) (Sequence, error) {
	key := k.prefix + "." + name + "." + field
	raw, ok := table[field]
	if !ok {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, &TableError{Key: key, Err: k.notArray()}
	}
	maps := make([]map[string]any, 0, len(items))
	for i, v := range items {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, &TableError{Key: key, Err: k.notTable(i + 1)}
		}
		maps = append(maps, m)
	}
	seq, err := FromMaps(maps)
	if err != nil {
		return nil, &TableError{Key: key, Err: err}
	}
	return seq, nil
}

// sortedNames orders the tables, so that a configuration with two bad
// tables always names the same one.
func sortedNames(tables map[string]map[string]any) []string {
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
