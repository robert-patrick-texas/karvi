package transform

import "testing"

func TestSequence(t *testing.T) {
	p := Sequence{Lowercase{}, CropToDot{}, AddSuffix{Suffix: ".example.gov"}}
	got, trace, err := p.Apply("SW1.EXAMPLE.NET")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sw1.example.gov" {
		t.Fatalf("got %q", got)
	}
	if len(trace) != 3 {
		t.Fatalf("trace=%d", len(trace))
	}
}
func TestReplaceCharsValidation(t *testing.T) {
	if (ReplaceChars{Pairs: []RunePair{{From: "ab", To: "x"}}}).Validate() == nil {
		t.Fatal("expected error")
	}
}
