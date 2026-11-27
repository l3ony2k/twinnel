package svchost

import "slices"

// ResolveProgram is argv with the program's path resolved through
// symlinks (eval: filepath.EvalSymlinks), done again at every start so a
// kernel switch (the "current" link moved) takes effect on the next one.
//
// The managed mihomo is started as kernels\mihomo\current\mihomo.exe,
// where current is a symlink to the version's directory. Started through
// the link, the process reports the link path as its own (GetModuleFileName,
// Go's os.Executable), so mihomo's TUN (sing-tun) adds its Windows
// Firewall rule for the link path, while the firewall (WFP) matches rules
// against the resolved image path, …\v1.2.3\mihomo.exe: the rule may never
// apply. Started by its resolved path, every one of them names the same
// file. A path that cannot be resolved is kept as it is.
func ResolveProgram(argv []string, eval func(string) (string, error)) []string {
	if len(argv) == 0 || eval == nil {
		return argv
	}
	real, err := eval(argv[0])
	if err != nil || real == "" || real == argv[0] {
		return argv
	}
	out := slices.Clone(argv)
	out[0] = real
	return out
}
