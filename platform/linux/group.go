package linux

import (
	"errors"
	"os/user"
	"strconv"
	"strings"

	"github.com/l3ony2k/twinnel/core/service"
)

// The helper's socket is for one group (SocketGroup, --group): the person
// who installs the helper must be in it, or their app cannot reach it.
// Installing (and upgrading) adds them to it when they are not, with
// usermod; the change reaches their processes at the next login.

// Usermod adds a user to a group (shadow-utils; /usr/sbin is /usr/bin on
// Arch and Fedora, so the path exists everywhere).
const Usermod = "/usr/sbin/usermod"

// Accounts reads the user and group databases (os/user in the CLI; fakes
// in tests). GroupIds lists a user's groups by gid, the primary one too.
type Accounts struct {
	LookupID    func(uid string) (*user.User, error)
	LookupGroup func(name string) (*user.Group, error)
	GroupIds    func(u *user.User) ([]string, error)
}

// SystemAccounts reads /etc/passwd and /etc/group (os/user).
var SystemAccounts = Accounts{
	LookupID:    user.LookupId,
	LookupGroup: user.LookupGroup,
	GroupIds:    func(u *user.User) ([]string, error) { return u.GroupIds() },
}

// InvokingUID is the uid of the person a Twinnel run acts for: pkexec's
// PKEXEC_UID, else sudo's SUDO_UID, else the real uid when not root.
// ok is false for root itself (nobody to add).
func InvokingUID(getenv func(string) string, uid int) (string, bool) {
	for _, k := range []string{"PKEXEC_UID", "SUDO_UID"} {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				return v, true
			}
			return "", false
		}
	}
	if uid > 0 {
		return strconv.Itoa(uid), true
	}
	return "", false
}

// GroupMember reports whether the user with uid is in group according to
// the group database (primary or supplementary). A group that does not
// exist yet has no members.
func GroupMember(acc Accounts, uid, group string) (username string, member bool, err error) {
	u, err := acc.LookupID(uid)
	if err != nil {
		return "", false, err
	}
	g, err := acc.LookupGroup(group)
	if err != nil {
		var unknown user.UnknownGroupError
		if errors.As(err, &unknown) {
			return u.Username, false, nil
		}
		return u.Username, false, err
	}
	if u.Gid == g.Gid {
		return u.Username, true, nil
	}
	ids, err := acc.GroupIds(u)
	if err != nil {
		return u.Username, false, err
	}
	for _, id := range ids {
		if id == g.Gid {
			return u.Username, true, nil
		}
	}
	return u.Username, false, nil
}

// AddToGroupStep adds username to group (usermod -a -G).
func AddToGroupStep(username, group string) service.Step {
	return service.Step{Kind: service.StepRun, Argv: []string{Usermod, "-a", "-G", group, username}}
}

// SocketAccessSteps are the steps that put the invoking user (uid) in the
// socket group: none when there is no such user (root alone) or they are
// a member already. username is who is added ("" for none).
func SocketAccessSteps(acc Accounts, uid string, ok bool, group string) (steps []service.Step, username string, err error) {
	if !ok {
		return nil, "", nil
	}
	name, member, err := GroupMember(acc, uid, group)
	if err != nil || member {
		return nil, "", err
	}
	return []service.Step{AddToGroupStep(name, group)}, name, nil
}

// SessionInGroup reports whether this process carries gid among its
// groups (getgroups and the effective gid): what the kernel checks on the
// socket. A user just added to the group lacks it until the next login.
func SessionInGroup(gid int, egid int, groups []int) bool {
	if gid == egid {
		return true
	}
	for _, g := range groups {
		if g == gid {
			return true
		}
	}
	return false
}
