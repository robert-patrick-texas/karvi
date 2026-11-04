package buildinfo

import (
	"slices"
	"testing"
)

func TestCurrentListsTheTwoTransports(t *testing.T) {
	want := []SSHTransport{
		{ID: "native", Name: "native", Linkage: "compiled-in"},
		{ID: "system", Name: "system", Linkage: "external-executable"},
	}
	if got := Current().SSHTransports; !slices.Equal(got, want) {
		t.Fatalf("transports %#v, want %#v", got, want)
	}
}
