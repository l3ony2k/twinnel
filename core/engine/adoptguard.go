package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/l3ony2k/duet/core/kernel"
	"github.com/l3ony2k/duet/core/takeover"
)

// adoptDirOwner decides who must own the adopted config dir from now on
// (the value of settings mihomo.config_dir_uid) and refuses setups a root
// helper must not write into:
//
//   - the config dir and the config file's dir have different owners;
//   - they are owned by root, but the setup was not found in a root-owned
//     LaunchDaemon (a LaunchAgent in a home dir is the user's to write, and
//     must not be able to point the helper at /etc or /Library);
//   - they are owned by a user other than the one whose setup is adopted
//     (the owner of the probe's home dir);
//   - a third user controls a directory above them.
//
// A helper not running as root (development) accepts anything and records
// the owner it sees.
func adoptDirOwner(probe takeover.Probe, m *takeover.MihomoSetup) (*int, error) {
	dirs := []string{filepath.Clean(m.Dir)}
	if d := filepath.Dir(filepath.Clean(m.Config)); !slices.Contains(dirs, d) {
		dirs = append(dirs, d)
	}
	owner := -1
	for _, d := range dirs {
		fi, err := os.Stat(d)
		if err != nil {
			return nil, err
		}
		uid, _, ok := statOwner(fi)
		if !ok {
			return nil, nil // platform without owners
		}
		if owner >= 0 && uid != owner {
			return nil, fmt.Errorf("%s and %s have different owners (uid %d and %d)", dirs[0], d, owner, uid)
		}
		owner = uid
	}
	if geteuid() != 0 {
		if owner == 0 {
			return nil, nil
		}
		return &owner, nil
	}
	if owner == 0 {
		svc := m.Service
		if svc == nil || svc.Kind != "launchd-daemon" {
			return nil, fmt.Errorf("the config dir %s is owned by root, but the setup was not found in a LaunchDaemon; duet only writes into a root-owned dir that a root-owned LaunchDaemon uses", dirs[0])
		}
		fi, err := os.Lstat(svc.Path)
		if err != nil {
			return nil, err
		}
		if uid, _, _ := statOwner(fi); uid != 0 || !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("the service definition %s is not a root-owned file", svc.Path)
		}
		if err := checkOwnedPath(filepath.Dir(svc.Path), 0, 0); err != nil {
			return nil, err
		}
		for _, d := range dirs {
			if err := checkOwnedPath(d, 0, 0); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	if home := probe.UserHome(); home != "" {
		fi, err := os.Stat(home)
		if err != nil {
			return nil, err
		}
		if uid, _, _ := statOwner(fi); uid != owner {
			return nil, fmt.Errorf("the config dir %s is owned by uid %d, not by the user whose setup is adopted (uid %d)", dirs[0], owner, uid)
		}
	}
	for _, d := range dirs {
		if err := checkOwnedPath(d, owner, owner); err != nil {
			return nil, err
		}
	}
	return &owner, nil
}

// binaryWarning explains an adopted mihomo binary that a non-root user can
// replace while a root service runs it.
func binaryWarning(bin string) string {
	if bin == "" {
		return ""
	}
	t, err := kernel.CheckExec(bin)
	if err != nil || t.Trusted {
		return ""
	}
	return fmt.Sprintf("the mihomo binary %s can be replaced without root (%s), and the service runs it as root: anything running as that user can become root. duet runs it only without root for its own checks; to close this, install a managed kernel (duet kernel install) and point the service at it", bin, t.Why)
}
