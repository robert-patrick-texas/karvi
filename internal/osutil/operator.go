package osutil

import (
	"os"
	"os/user"
	"strconv"

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
	return credentials.Operator{Username: name, UID: uid, PrimaryGID: gid, Groups: names, Home: u.HomeDir}, nil
}
func RecordOperator(o credentials.Operator) records.Operator {
	return records.Operator{Username: o.Username, UID: o.UID, PrimaryGID: o.PrimaryGID, Groups: append([]string(nil), o.Groups...)}
}
