package osutil

import (
	"math"
	"os"
	"os/user"
	"strconv"
	"strings"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/records"
)

func CurrentOperator() (credentials.Operator, error) {
	u, err := user.Current()
	if err != nil {
		return credentials.Operator{}, errorcodes.Errorf("operator_identity_unavailable", "look up current user: %w", err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return credentials.Operator{}, errorcodes.Errorf("operator_identity_unavailable", "parse current UID: %w", err)
	}
	gid, _ := strconv.Atoi(u.Gid)
	groups, _ := u.GroupIds()
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		if gr, e := user.LookupGroupId(g); e == nil {
			names = append(names, gr.Name)
		}
	}
	name := u.Username
	if name == "" {
		name = os.Getenv("USER")
	}
	if name == "" {
		return credentials.Operator{}, errorcodes.Errorf("operator_identity_unavailable", "operator username unavailable")
	}
	return credentials.Operator{Username: name, UID: uid, PrimaryGID: gid, Groups: names, Home: u.HomeDir, LoginUser: LoginUser(0, uid)}, nil
}

// LoginUser is the account the kernel's login UID of process pid names
// (/proc/PID/loginuid, pid 0 this process), when the login UID is set and is
// not uid: under sudo the account that logged in, where uid is the one the
// work runs as. It is "" when the login UID is unset (a systemd service, a
// container), unreadable, or uid's own, and the number when no account
// carries it.
func LoginUser(pid, uid int) string {
	path := "/proc/self/loginuid"
	if pid > 0 {
		path = "/proc/" + strconv.Itoa(pid) + "/loginuid"
	}
	return loginUserFrom(path, uid)
}

func loginUserFrom(path string, uid int) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 32)
	if err != nil || n == math.MaxUint32 || n == uint64(uid) {
		return ""
	}
	id := strconv.FormatUint(n, 10)
	if u, err := user.LookupId(id); err == nil && u.Username != "" {
		return u.Username
	}
	return id
}
func RecordOperator(o credentials.Operator) records.Operator {
	return records.Operator{Username: o.Username, UID: o.UID, PrimaryGID: o.PrimaryGID, Groups: append([]string(nil), o.Groups...)}
}
