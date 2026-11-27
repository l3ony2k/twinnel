//go:build windows

package windows

import (
	"context"
	"errors"
	"unsafe"

	xwin "golang.org/x/sys/windows"

	"github.com/l3ony2k/twinnel/core/foreign"
)

var (
	modIphlpapi             = xwin.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTcpTable = modIphlpapi.NewProc("GetExtendedTcpTable")
)

const tcpTableOwnerPIDListener = 3 // TCP_TABLE_OWNER_PID_LISTENER

// PortOwner finds the process listening on TCP port: GetExtendedTcpTable
// (IPv4, then IPv6) for the PID, then its image path and its parent's
// (the helper runs as SYSTEM, so every process is readable). Read-only.
// nil, nil when nothing listens.
func PortOwner(ctx context.Context, port int) (*foreign.Process, error) {
	pid, err := listenerPID(port)
	if err != nil || pid == 0 {
		return nil, err
	}
	p := &foreign.Process{PID: int(pid)}
	p.Exe = imagePath(pid)
	if pp := parentPID(pid); pp > 0 {
		p.PPID = int(pp)
		p.ParentExe = imagePath(pp)
	}
	return p, nil
}

func listenerPID(port int) (uint32, error) {
	for _, family := range []uint32{xwin.AF_INET, xwin.AF_INET6} {
		var size uint32
		r, _, _ := procGetExtendedTcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, uintptr(family), tcpTableOwnerPIDListener, 0)
		if r != uintptr(xwin.ERROR_INSUFFICIENT_BUFFER) || size == 0 {
			continue
		}
		buf := make([]byte, size+1024)
		size = uint32(len(buf))
		r, _, _ = procGetExtendedTcpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, uintptr(family), tcpTableOwnerPIDListener, 0)
		if r != 0 {
			return 0, errors.New("GetExtendedTcpTable failed")
		}
		if pid := findListener(buf[:size], family == xwin.AF_INET6, port); pid != 0 {
			return pid, nil
		}
	}
	return 0, nil
}

func imagePath(pid uint32) string {
	h, err := xwin.OpenProcess(xwin.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer xwin.CloseHandle(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	if err := xwin.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return xwin.UTF16ToString(buf[:size])
}

func parentPID(pid uint32) uint32 {
	snap, err := xwin.CreateToolhelp32Snapshot(xwin.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer xwin.CloseHandle(snap)
	var e xwin.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = xwin.Process32First(snap, &e); err == nil; err = xwin.Process32Next(snap, &e) {
		if e.ProcessID == pid {
			return e.ParentProcessID
		}
	}
	return 0
}
