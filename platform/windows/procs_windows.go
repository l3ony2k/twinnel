//go:build windows

package windows

import (
	"context"
	"time"
	"unsafe"

	xwin "golang.org/x/sys/windows"

	"github.com/l3ony2k/twinnel/core/status"
)

// List reads the system's process list (NtQuerySystemInformation's
// SystemProcessInformation, as Task Manager does).
func (l *ProcessLister) List(ctx context.Context) ([]status.ProcessEntry, error) {
	size := uint32(512 << 10)
	var buf []byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		buf = make([]byte, size)
		err := xwin.NtQuerySystemInformation(xwin.SystemProcessInformation, unsafe.Pointer(&buf[0]), size, &size)
		if err == nil {
			break
		}
		if err != xwin.STATUS_INFO_LENGTH_MISMATCH {
			return nil, err
		}
		size += 64 << 10 // processes start while we ask
	}
	now := time.Now()
	var samples []procSample
	for off := uint32(0); int(off)+int(unsafe.Sizeof(xwin.SYSTEM_PROCESS_INFORMATION{})) <= len(buf); {
		p := (*xwin.SYSTEM_PROCESS_INFORMATION)(unsafe.Pointer(&buf[off]))
		samples = append(samples, procSample{
			pid:     int(p.UniqueProcessID),
			name:    p.ImageName.String(),
			private: p.WorkingSetPrivateSize,
			ws:      int64(p.WorkingSetSize),
			cpu:     p.UserTime + p.KernelTime,
			create:  p.CreateTime,
		})
		if p.NextEntryOffset == 0 {
			break
		}
		off += p.NextEntryOffset
	}
	return l.entries(samples, now), nil
}
