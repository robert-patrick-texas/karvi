package osutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

var jobTestNow = time.Date(2026, 9, 23, 21, 51, 6, 830975000, time.FixedZone("EDT", -4*3600))

// claimJob is ClaimActivityID for a job that writes files under root,
// its claims in a folder of its own.
func claimJob(t *testing.T, claims, root string, mode os.FileMode, loc *time.Location) (string, string, error) {
	t.Helper()
	return ClaimActivityID(claims, 0o600, root, mode, jobTestNow, loc)
}

// TestClaimActivityIDSequencesWithinOneSecond: the first job of a second is
// 00, the next 01, each an empty claim created exclusively in the claims
// folder at the given mode and an exclusively created directory under the
// day folder of the stamp at the directory mode.
func TestClaimActivityIDSequencesWithinOneSecond(t *testing.T) {
	claims, root := t.TempDir(), t.TempDir()
	loc := jobTestNow.Location()
	var ids []string
	for i := 0; i < 3; i++ {
		id, dir, err := claimJob(t, claims, root, 0o700, loc)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(root, "260923", id); dir != want {
			t.Errorf("dir %s, want %s", dir, want)
		}
		fi, err := os.Stat(dir)
		if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v %v", dir, fi, err)
		}
		claim := filepath.Join(claims, id+ClaimExtension)
		if fi, err := os.Stat(claim); err != nil || fi.Size() != 0 || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v %v", claim, fi, err)
		}
		ids = append(ids, id)
	}
	if want := []string{"260923-215106-00", "260923-215106-01", "260923-215106-02"}; ids[0] != want[0] || ids[1] != want[1] || ids[2] != want[2] {
		t.Errorf("ids %v, want %v", ids, want)
	}
	// The stamp is the effective timezone's wall clock.
	if id, _, err := claimJob(t, claims, root, 0o750, time.UTC); err != nil || id != "260924-015106-00" {
		t.Errorf("UTC: %s %v", id, err)
	}
}

// TestClaimActivityIDAcrossActivities: activities with and without a job
// directory draw from one sequence in their claims folder: two jobs of
// `--nof`, a login or a dry run, and a job that writes files, all in one
// second, take 00 to 03, and only the last has a directory.
func TestClaimActivityIDAcrossActivities(t *testing.T) {
	claims, root := t.TempDir(), t.TempDir()
	loc := jobTestNow.Location()
	var ids []string
	for i := 0; i < 3; i++ {
		id, dir, err := ClaimActivityID(claims, 0o640, "", 0o700, jobTestNow, loc)
		if err != nil || dir != "" {
			t.Fatalf("claim alone: %s %q %v", id, dir, err)
		}
		ids = append(ids, id)
	}
	id, dir, err := claimJob(t, claims, root, 0o700, loc)
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, id)
	if want := "260923-215106-00 260923-215106-01 260923-215106-02 260923-215106-03"; strings.Join(ids, " ") != want {
		t.Errorf("ids %v, want %s", ids, want)
	}
	entries, err := os.ReadDir(filepath.Join(root, "260923"))
	if err != nil || len(entries) != 1 || filepath.Join(root, "260923", entries[0].Name()) != dir {
		t.Errorf("job folders %v %v, want %s alone", entries, err, dir)
	}
}

// TestClaimActivityIDNeedsBothNamesFree: a job directory already present
// without its claim (another host's job under a shared output root) moves
// the job to the next sequence, and the claim made on the way stays, so
// the ID is not drawn again; a claim present without its directory moves
// it on too.
func TestClaimActivityIDNeedsBothNamesFree(t *testing.T) {
	claims, root := t.TempDir(), t.TempDir()
	loc := jobTestNow.Location()
	if err := os.MkdirAll(filepath.Join(root, "260923", "260923-215106-00"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claims, "260923-215106-01"+ClaimExtension), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	id, dir, err := claimJob(t, claims, root, 0o700, loc)
	if err != nil || id != "260923-215106-02" || dir != filepath.Join(root, "260923", id) {
		t.Fatalf("id %s dir %s err %v", id, dir, err)
	}
	if _, err := os.Stat(filepath.Join(claims, "260923-215106-00"+ClaimExtension)); err != nil {
		t.Errorf("the claim made on the way is gone: %v", err)
	}
	if id, _, err := ClaimActivityID(claims, 0o600, "", 0o700, jobTestNow, loc); err != nil || id != "260923-215106-03" {
		t.Errorf("next claim: %s %v", id, err)
	}
}

// TestClaimActivityIDRefusal: a claim that cannot be created is
// activity_id_generation_failed naming its path, and nothing is made under
// the output root.
func TestClaimActivityIDRefusal(t *testing.T) {
	claims := filepath.Join(t.TempDir(), "absent")
	root := t.TempDir()
	_, _, err := claimJob(t, claims, root, 0o700, jobTestNow.Location())
	if errorcodes.Of(err) != "activity_id_generation_failed" || !strings.Contains(err.Error(), filepath.Join(claims, "260923-215106-00"+ClaimExtension)) {
		t.Errorf("refusal: %v", err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Errorf("the output root gained %v", entries)
	}
}

// TestSweepClaims: a claim removes the operator's own claims of earlier
// days and keeps today's, a later day's (a clock stepped back), and every
// file that is not a claim.
func TestSweepClaims(t *testing.T) {
	claims := t.TempDir()
	kept := []string{"260923-101010-00.id", "260924-000000-00.id", "260922-101010-00.json", "260922-101010-00.idx", "notes.id"}
	gone := []string{"260922-235959-zz.id", "251231-000000-00.id"}
	for _, name := range append(append([]string{}, kept...), gone...) {
		if err := os.WriteFile(filepath.Join(claims, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	id, _, err := ClaimActivityID(claims, 0o600, "", 0o700, jobTestNow, jobTestNow.Location())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range append(kept, id+ClaimExtension) {
		if _, err := os.Stat(filepath.Join(claims, name)); err != nil {
			t.Errorf("%s removed: %v", name, err)
		}
	}
	for _, name := range gone {
		if _, err := os.Stat(filepath.Join(claims, name)); !os.IsNotExist(err) {
			t.Errorf("%s kept: %v", name, err)
		}
	}
}

// TestJobSequenceCoversTheAlphabet: 36 letters and digits in order, 1296
// per second, and the loop ends with a coded error when all are taken.
func TestJobSequenceCoversTheAlphabet(t *testing.T) {
	if jobSequence(0) != "00" || jobSequence(9) != "09" || jobSequence(10) != "0a" || jobSequence(36) != "10" || jobSequence(JobSequences-1) != "zz" {
		t.Errorf("sequence %s %s %s %s %s", jobSequence(0), jobSequence(9), jobSequence(10), jobSequence(36), jobSequence(JobSequences-1))
	}
	claims := t.TempDir()
	for n := 0; n < JobSequences; n++ {
		if err := os.WriteFile(filepath.Join(claims, "260923-215106-"+jobSequence(n)+ClaimExtension), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := claimJob(t, claims, t.TempDir(), 0o700, jobTestNow.Location()); errorcodes.Of(err) != "activity_id_generation_failed" {
		t.Errorf("exhausted: %v", err)
	}
}

// TestReleaseJobIDRemovesOnlyAnEmptyDirectory: a reservation that gained
// files is never touched, and the claim stays whatever the release does.
func TestReleaseJobIDRemovesOnlyAnEmptyDirectory(t *testing.T) {
	claims, root := t.TempDir(), t.TempDir()
	loc := jobTestNow.Location()
	id, dir, err := claimJob(t, claims, root, 0o700, loc)
	if err != nil {
		t.Fatal(err)
	}
	ReleaseJobID(dir)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("empty reservation remains: %v", err)
	}
	// The day folder the reservation alone had made goes with it.
	if _, err := os.Stat(filepath.Dir(dir)); !os.IsNotExist(err) {
		t.Errorf("empty day folder remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(claims, id+ClaimExtension)); err != nil {
		t.Errorf("the claim went with the release: %v", err)
	}
	ReleaseJobID(dir) // absent: nothing to do
	ReleaseJobID("")
	// The released ID is not drawn again.
	next, dir, err := claimJob(t, claims, root, 0o700, loc)
	if err != nil || next == id {
		t.Fatalf("after the release: %s %v", next, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "commands.jsonl"), []byte("{}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	ReleaseJobID(dir)
	if _, err := os.Stat(filepath.Join(dir, "commands.jsonl")); err != nil {
		t.Errorf("a directory with files was touched: %v", err)
	}
	// A second reservation beside a job: its release keeps the day folder,
	// which holds the job.
	_, other, err := claimJob(t, claims, root, 0o700, loc)
	if err != nil {
		t.Fatal(err)
	}
	ReleaseJobID(other)
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("the day folder with a job was removed: %v", err)
	}
}

// TestClaimJobDirectory: the reserved empty directory is
// accepted, an absent one is created, and one holding entries is refused
// as output_directory_in_use before anything is written.
func TestClaimJobDirectory(t *testing.T) {
	root := t.TempDir()
	_, dir, err := claimJob(t, t.TempDir(), root, 0o700, jobTestNow.Location())
	if err != nil {
		t.Fatal(err)
	}
	if err := ClaimJobDirectory(dir, 0o700); err != nil {
		t.Errorf("reserved: %v", err)
	}
	absent := filepath.Join(root, "260923", "260923-215106-07")
	if err := ClaimJobDirectory(absent, 0o750); err != nil {
		t.Errorf("absent: %v", err)
	}
	if fi, err := os.Stat(absent); err != nil || fi.Mode().Perm() != 0o750 {
		t.Errorf("absent created: %v %v", fi, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), []byte("{}"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := ClaimJobDirectory(dir, 0o700); errorcodes.Of(err) != "output_directory_in_use" {
		t.Errorf("in use: %v", err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := ClaimJobDirectory(file, 0o700); errorcodes.Of(err) != "output_directory_not_writable" {
		t.Errorf("not a folder: %v", err)
	}
}

// TestJobIDTime reads the stamp in the given zone and codes a malformed ID.
func TestJobIDTime(t *testing.T) {
	at, err := JobIDTime("260923-215106-0z", time.UTC)
	if err != nil || !at.Equal(time.Date(2026, 9, 23, 21, 51, 6, 0, time.UTC)) {
		t.Errorf("%v %v", at, err)
	}
	for _, bad := range []string{"", "260923-215106", "260923-215106-000", "260923T215106-00", "261323-215106-00"} {
		if _, err := JobIDTime(bad, time.UTC); errorcodes.Of(err) != "job_request_malformed" {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

// TestClaimActivityIDKeepsSetgid: a job directory inside a setgid day
// folder keeps the bit as the folder helper does.
func TestClaimActivityIDKeepsSetgid(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, os.ModeSetgid|0o770); err != nil {
		t.Fatal(err)
	}
	_, dir, err := ClaimActivityID(t.TempDir(), 0o600, root, 0o750, time.Date(2026, 9, 24, 2, 0, 0, 0, time.UTC), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	// The day folder takes the setgid root's own bits, the job folder the
	// configured mode; both keep the bit.
	for path, want := range map[string]os.FileMode{filepath.Dir(dir): 0o770, dir: 0o750} {
		fi, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode()&os.ModeSetgid == 0 || fi.Mode().Perm() != want {
			t.Fatalf("%s: %v, want setgid and %o", path, fi.Mode(), want)
		}
	}
}

// TestReserveSequence: the one loop behind every claim: a name reported
// taken is passed over, a name taken ends the search with it, another
// error ends the search as it is, and every sequence taken is the coded
// error.
func TestReserveSequence(t *testing.T) {
	loc := jobTestNow.Location()
	taken := map[string]bool{"260923-215106-00": true, "260923-215106-01": true}
	var tried []string
	id, err := reserveSequence(jobTestNow, loc, "here", func(id string) error {
		tried = append(tried, id)
		if taken[id] {
			return os.ErrExist
		}
		return nil
	})
	if err != nil || id != "260923-215106-02" || len(tried) != 3 {
		t.Errorf("id %s err %v tried %v", id, err, tried)
	}
	fault := errors.New("disk gone")
	if _, err := reserveSequence(jobTestNow, loc, "here", func(string) error { return fault }); !errors.Is(err, fault) {
		t.Errorf("a fault should end the search: %v", err)
	}
	_, err = reserveSequence(jobTestNow, loc, "here", func(string) error { return os.ErrExist })
	if errorcodes.Of(err) != "activity_id_generation_failed" || !strings.Contains(err.Error(), "under here") {
		t.Errorf("every sequence taken: %v", err)
	}
}
