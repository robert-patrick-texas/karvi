package executionplan

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// Configuration is the plan's configuration block (schema 12): every value
// of the client's resolved configuration by key, the object `config show
// --format json` prints as values, which sources.config_digest digests. The
// daemon builds the job's configuration from it. It decodes with numbers as
// written, an integer literal an int64 and any other a float64, so the
// values keep the types the client's load gave them.
type Configuration map[string]any

// UnmarshalJSON decodes the block with its numbers exact.
func (c *Configuration) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	if raw == nil {
		*c = nil
		return nil
	}
	out := make(Configuration, len(raw))
	for k, v := range raw {
		out[k] = exactNumbers(v)
	}
	*c = out
	return nil
}

// exactNumbers replaces each json.Number in v with an int64 when it is an
// integer literal, else a float64.
func exactNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := strconv.ParseInt(x.String(), 10, 64); err == nil {
			return i
		}
		f, _ := x.Float64()
		return f
	case []any:
		for i := range x {
			x[i] = exactNumbers(x[i])
		}
		return x
	case map[string]any:
		for k := range x {
			x[k] = exactNumbers(x[k])
		}
		return x
	}
	return v
}
