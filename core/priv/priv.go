// Package priv answers "may this process change the system?": root on
// macOS and Linux; an elevated token or LocalSystem on Windows. Code that
// decides whether to run a plan for real or as a dry run asks Elevated
// instead of comparing os.Geteuid with 0, which is always -1 on Windows.
package priv

// Elevated reports whether the process runs with full privileges.
func Elevated() bool { return elevated() }
