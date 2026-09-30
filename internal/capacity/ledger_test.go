package capacity

import (
	"context"
	"testing"
)

func TestAcquireRelease(t *testing.T) {
	m, err := New(t.TempDir(), "", "job", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, err := m.Acquire(context.Background(), "device", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
}
