//go:build windows

package scm

import (
	"context"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

// serviceInfo reads a service's command line, state and process id
// (read-only).
func serviceInfo(_ context.Context, label string) (string, bool, int, error) {
	m, err := connectReadOnly()
	if err != nil {
		return "", false, 0, err
	}
	defer m.Disconnect()
	n, _ := windows.UTF16PtrFromString(label)
	h, err := windows.OpenService(m.Handle, n, windows.SERVICE_QUERY_STATUS|windows.SERVICE_QUERY_CONFIG)
	if err != nil {
		return "", false, 0, err
	}
	s := mgrService(label, h)
	defer s.Close()
	cfg, err := s.Config()
	if err != nil {
		return "", false, 0, err
	}
	q, err := s.Query()
	if err != nil {
		return cfg.BinaryPathName, false, 0, nil
	}
	return cfg.BinaryPathName, q.State == svc.Running, int(q.ProcessId), nil
}

// processImage is the executable a process runs ("" when this user may
// not read it).
func processImage(pid int) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}
