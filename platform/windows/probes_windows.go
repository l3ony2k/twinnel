//go:build windows

package windows

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi          = windows.NewLazySystemDLL("iphlpapi.dll")
	procIcmpCreate    = iphlpapi.NewProc("IcmpCreateFile")
	procIcmpClose     = iphlpapi.NewProc("IcmpCloseHandle")
	procIcmpSendEcho  = iphlpapi.NewProc("IcmpSendEcho")
	errNoEchoReply    = errors.New("no echo reply")
	icmpStatusSuccess = uint32(0) // IP_SUCCESS
)

// icmpEchoReply is ICMP_ECHO_REPLY (32-bit layout of IP_OPTION_INFORMATION
// padded for 64-bit: the reply buffer only needs to be large enough).
type icmpEchoReply struct {
	Address       uint32
	Status        uint32
	RoundTripTime uint32
	DataSize      uint16
	Reserved      uint16
	Data          uintptr
	Options       [16]byte
}

// icmpEcho sends one echo with IcmpSendEcho, bounded by ctx's deadline
// (at most a second).
func icmpEcho(ctx context.Context, ip string) error {
	a, err := netip.ParseAddr(ip)
	if err != nil || !a.Is4() {
		return fmt.Errorf("ping: %q is not an IPv4 address", ip)
	}
	timeout := time.Second
	if dl, ok := ctx.Deadline(); ok {
		if d := time.Until(dl); d < timeout {
			timeout = d
		}
	}
	if timeout <= 0 {
		return errNoEchoReply
	}
	h, _, cerr := procIcmpCreate.Call()
	if windows.Handle(h) == windows.InvalidHandle {
		return fmt.Errorf("IcmpCreateFile: %v", cerr)
	}
	defer procIcmpClose.Call(h)
	b := a.As4()
	dst := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
	payload := []byte("twnl")
	reply := make([]byte, int(unsafe.Sizeof(icmpEchoReply{}))+len(payload)+8+64)
	n, _, _ := procIcmpSendEcho.Call(h, uintptr(dst),
		uintptr(unsafe.Pointer(&payload[0])), uintptr(len(payload)), 0,
		uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), uintptr(timeout.Milliseconds()))
	if n == 0 {
		return errNoEchoReply
	}
	r := (*icmpEchoReply)(unsafe.Pointer(&reply[0]))
	if r.Status != icmpStatusSuccess {
		return fmt.Errorf("echo status %d", r.Status)
	}
	return nil
}
