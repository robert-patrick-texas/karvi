package tabular

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestHeaderMappingAndComments(t *testing.T) {
	s := "\ufeffHost-Name, Platform,ignored\r\n#comment\r\n sw1 , iosxe ,x\r\n"
	it, err := (CSVReader{}).Read(context.Background(), strings.NewReader(s), Spec{Requested: []string{"name", "platform"}, Aliases: map[string][]string{"name": {"host_name"}}, Mandatory: []string{"name", "platform"}, Trim: true})
	if err != nil {
		t.Fatal(err)
	}
	if !it.Next() {
		t.Fatalf("no row: %v", it.Err())
	}
	r := it.Record()
	if r.Values["name"] != "sw1" || r.Values["platform"] != "iosxe" {
		t.Fatalf("%v", r.Values)
	}
}
func TestShortRowSkipped(t *testing.T) {
	warn := 0
	it, err := (CSVReader{}).Read(context.Background(), strings.NewReader("a,b\nx\nx,y\n"), Spec{Requested: []string{"a", "b"}, Mandatory: []string{"a", "b"}, Warn: func(string) { warn++ }})
	if err != nil {
		t.Fatal(err)
	}
	if !it.Next() {
		t.Fatal("expected row")
	}
	if warn != 1 {
		t.Fatalf("warn=%d", warn)
	}
}

// TestTrimIsTheCallersChoice: with Trim off a
// cell is kept verbatim, so a secret that begins or ends with a space
// survives; with Trim on every cell is stripped, inventory's behaviour.
// Header names are matched without their white space either way.
func TestTrimIsTheCallersChoice(t *testing.T) {
	const body = "username , password\n svc , pass word \n\"quoted\",\"  padded  \"\n"
	for _, tc := range []struct {
		trim bool
		want [][2]string
	}{
		{false, [][2]string{{" svc ", " pass word "}, {"quoted", "  padded  "}}},
		{true, [][2]string{{"svc", "pass word"}, {"quoted", "padded"}}},
	} {
		it, err := (CSVReader{}).Read(context.Background(), strings.NewReader(body), Spec{Requested: []string{"username", "password"}, Mandatory: []string{"username"}, Trim: tc.trim})
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range tc.want {
			if !it.Next() {
				t.Fatalf("trim=%v row %d: %v", tc.trim, i, it.Err())
			}
			v := it.Record().Values
			if v["username"] != want[0] || v["password"] != want[1] {
				t.Errorf("trim=%v row %d: %q %q, want %q", tc.trim, i, v["username"], v["password"], want)
			}
		}
	}
}

// TestByteOrderMark: the mark is stripped from the start of the stream in
// both modes, before the CSV parser sees it; one inside the file is data.
func TestByteOrderMark(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		spec       Spec
	}{
		{"numeric", "\ufeffsw1,iosxe\n", Spec{Mode: "numeric", NumericMappings: map[string]int{"name": 1, "platform": 2}}},
		{"header", "\ufeffname,platform\nsw1,iosxe\n", Spec{}},
		{"header, quoted first field", "\ufeff\"name\",platform\nsw1,iosxe\n", Spec{}},
		{"comment first", "\ufeff# exported\nname,platform\nsw1,iosxe\n", Spec{}},
	} {
		spec := tc.spec
		spec.Requested, spec.Mandatory = []string{"name", "platform"}, []string{"name"}
		it, err := (CSVReader{}).Read(context.Background(), strings.NewReader(tc.body), spec)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !it.Next() || it.Record().Values["name"] != "sw1" || it.Record().Values["platform"] != "iosxe" {
			t.Errorf("%s: %q %v", tc.name, it.Record().Values, it.Err())
		}
	}
	it, err := (CSVReader{}).Read(context.Background(), strings.NewReader("sw1,x\n\ufeffsw2,y\n"), Spec{Mode: "numeric", Requested: []string{"name"}, NumericMappings: map[string]int{"name": 1}})
	if err != nil {
		t.Fatal(err)
	}
	it.Next()
	if !it.Next() || it.Record().Values["name"] != "\ufeffsw2" {
		t.Errorf("a mark inside the file was stripped: %q", it.Record().Values["name"])
	}
}

// TestShortRowIsAnErrorWhenAsked: the credential CSV's policy; the line is
// named.
func TestShortRowIsAnErrorWhenAsked(t *testing.T) {
	it, err := (CSVReader{}).Read(context.Background(), strings.NewReader("a,b\nx,y\nx\n"), Spec{Requested: []string{"a", "b"}, ShortRowPolicy: "error"})
	if err != nil {
		t.Fatal(err)
	}
	if !it.Next() {
		t.Fatal(it.Err())
	}
	if it.Next() || it.Err() == nil || !strings.Contains(it.Err().Error(), "line 3") {
		t.Errorf("short row: %v", it.Err())
	}
}

// TestCheckHeader: the caller's hook sees every header of a header-mode
// file, mapped or not, in column order and as written; its error is the
// read's error, unchanged; numeric mode never calls it.
func TestCheckHeader(t *testing.T) {
	var seen []string
	record := func(column int, header string) error {
		seen = append(seen, fmt.Sprintf("%d=%s", column, header))
		return nil
	}
	if _, err := (CSVReader{}).Read(context.Background(), strings.NewReader("Name, Extra-One ,site\nsw1,x,nyc\n"), Spec{Requested: []string{"name", "site"}, Trim: true, CheckHeader: record}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(seen, "|"); got != "1=Name|2= Extra-One |3=site" {
		t.Fatalf("headers seen: %s", got)
	}
	refuse := errors.New("the caller's own error")
	_, err := (CSVReader{}).Read(context.Background(), strings.NewReader("name,extra\nsw1,x\n"), Spec{Requested: []string{"name"}, CheckHeader: func(column int, _ string) error {
		if column == 2 {
			return refuse
		}
		return nil
	}})
	if err != refuse {
		t.Fatalf("err = %v, want the hook's error unchanged", err)
	}
	seen = nil
	if _, err := (CSVReader{}).Read(context.Background(), strings.NewReader("sw1,x\n"), Spec{Mode: "numeric", Requested: []string{"name"}, NumericMappings: map[string]int{"name": 1}, CheckHeader: record}); err != nil || len(seen) != 0 {
		t.Fatalf("numeric mode: err=%v seen=%v", err, seen)
	}
}
