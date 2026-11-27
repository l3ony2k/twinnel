//go:build windows

package scm

import "golang.org/x/sys/windows"

// ServiceCommandLine is an installed service's command line (read-only).
func ServiceCommandLine(name string) (string, error) {
	m, err := connectReadOnly()
	if err != nil {
		return "", err
	}
	defer m.Disconnect()
	n, _ := windows.UTF16PtrFromString(name)
	h, err := windows.OpenService(m.Handle, n, windows.SERVICE_QUERY_CONFIG)
	if err != nil {
		return "", err
	}
	defer windows.CloseServiceHandle(h)
	s := mgrService(name, h)
	cfg, err := s.Config()
	if err != nil {
		return "", err
	}
	return cfg.BinaryPathName, nil
}
