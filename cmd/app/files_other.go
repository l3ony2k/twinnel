//go:build !windows

package main

import (
	"os/exec"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/twinnel/core/legacy"
)

// exeName is a program's file name: the name itself.
func exeName(name string) string { return name }

// Paths that tell an installed helper from none (see `twinnel helper install
// --dry-run`): the binary and its LaunchDaemon plist.
// A helper from before the rename counts as installed too: it runs until
// 更新 migrates it.
var helperInstalledPaths = []string{
	"/Library/PrivilegedHelperTools/com.l3ony2k.twinnel.helper",
	"/Library/LaunchDaemons/com.l3ony2k.twinnel.helper.plist",
	legacy.HelperTools,
	legacy.HelperPlist,
}

// removeAppBundle moves the app to the Trash (完全移除).
func removeAppBundle(path string) { mygo.Shell.TrashItem(path) }

// hideConsole: nothing to hide outside Windows.
func hideConsole(*exec.Cmd) {}
