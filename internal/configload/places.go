package configload

import (
	"sort"

	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// absolutePlaces makes every value of a key the registry marks as a path
// absolute, the load's last change before its digest: `~` the operator's
// home and a relative path from the working directory, by
// osutil.ResolvePath, so `config show` prints the place a job records and a
// process in another working directory reads the same one. An empty value
// and the key's words (auto, none) stay as written, as does every element of
// a list that is not a string. Validation has judged the values as written.
func (l *loader) absolutePlaces() error {
	keys := make([]string, 0, len(l.snap.Values))
	for k := range l.snap.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		words, ok := configschema.Place(key)
		if !ok {
			continue
		}
		v := l.snap.Values[key]
		data, err := absolutePlace(v.Data, words, l.home)
		if err != nil {
			return l.semanticErr("config_working_directory_unavailable", key, err)
		}
		v.Data = data
		l.snap.Values[key] = v
	}
	return nil
}

// absolutePlace is one value made absolute: a string, or each string of a
// list.
func absolutePlace(data any, words []string, home string) (any, error) {
	switch x := data.(type) {
	case string:
		if x == "" || oneOfWords(x, words) {
			return x, nil
		}
		return osutil.ResolvePath(x, home)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			p, err := absolutePlace(e, words, home)
			if err != nil {
				return nil, err
			}
			out[i] = p
		}
		return out, nil
	case []string:
		out := make([]string, len(x))
		for i, e := range x {
			p, err := absolutePlace(e, words, home)
			if err != nil {
				return nil, err
			}
			out[i] = p.(string)
		}
		return out, nil
	}
	return data, nil
}

func oneOfWords(s string, words []string) bool {
	for _, w := range words {
		if s == w {
			return true
		}
	}
	return false
}
