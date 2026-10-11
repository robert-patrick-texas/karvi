package credentials

import "testing"

// TestFill is EXAMPLES chapter 56, issue 1: a template's every %operator%
// is replaced, as text, by the request's name; %s, a site's other text, and
// a template without one stay as written.
func TestFill(t *testing.T) {
	req := ResolveRequest{Operator: Operator{Username: "j.smith"}}
	for template, want := range map[string]string{
		"karvi:credential:%operator%": "karvi:credential:j.smith",
		"KARVI_%operator%_%operator%": "KARVI_j.smith_j.smith",
		"%operator%":                  "j.smith",
		"karvi:shared":                "karvi:shared",
		"%s-adm":                      "%s-adm",
		"svc-%d":                      "svc-%d",
		"%Operator%":                  "%Operator%",
		"":                            "",
	} {
		if got := req.Fill(template); got != want {
			t.Errorf("%q: %q, want %q", template, got, want)
		}
	}
}
