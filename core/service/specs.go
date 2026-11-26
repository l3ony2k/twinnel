package service

import "path/filepath"

// MihomoSpec is the mihomo service: `mihomo -d <confDir> -f <confFile>`,
// restarted by the system if it exits.
func MihomoSpec(label, bin, confDir, confFile, logDir string) Spec {
	s := Spec{
		Name: "mihomo", Label: label, Program: bin,
		Args:      []string{"-d", confDir, "-f", confFile},
		RunAtLoad: true, KeepAlive: true, ThrottleSeconds: 10,
	}
	if logDir != "" {
		s.StdoutPath = filepath.Join(logDir, "mihomo.out.log")
		s.StderrPath = filepath.Join(logDir, "mihomo.err.log")
	}
	return s
}

// TailscaledSpec is the open-source tailscaled as a system daemon, like
// `tailscaled install-system-daemon` does.
func TailscaledSpec(label, bin string) Spec {
	return Spec{
		Name: "tailscaled", Label: label, Program: bin,
		RunAtLoad: true, KeepAlive: true,
	}
}

// HelperSpec is the duet helper daemon.
func HelperSpec(label, bin, stateDir, socket, logDir string) Spec {
	s := Spec{
		Name: "helper", Label: label, Program: bin,
		Args:      []string{"--state-dir", stateDir, "--socket", socket},
		RunAtLoad: true, KeepAlive: true, ThrottleSeconds: 5,
	}
	if logDir != "" {
		s.StdoutPath = filepath.Join(logDir, "helper.log")
		s.StderrPath = filepath.Join(logDir, "helper.log")
	}
	return s
}
