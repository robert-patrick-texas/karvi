package buildinfo

import "testing"

func TestCurrentReportsRegisteredSSHTransports(t *testing.T) {
	const testID = "test-scrapligo-v2"
	registerSSHTransport(SSHTransport{ID: testID, Name: "scrapligo", Version: "2.0.0", Linkage: "shared-library"})
	defer func() {
		transportRegistry.Lock()
		delete(transportRegistry.items, testID)
		transportRegistry.Unlock()
	}()
	got := Current().SSHTransports
	foundSystem := false
	foundTest := false
	for _, item := range got {
		if item.ID == "system" && item.Linkage == "external-executable" {
			foundSystem = true
		}
		if item.ID == testID && item.Name == "scrapligo" && item.Version == "2.0.0" && item.Linkage == "shared-library" {
			foundTest = true
		}
	}
	if !foundSystem || !foundTest {
		t.Fatalf("registered transports missing: %#v", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].ID > got[i].ID {
			t.Fatalf("transports not deterministically sorted: %#v", got)
		}
	}
}
