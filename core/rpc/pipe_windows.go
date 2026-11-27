//go:build windows

package rpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"

	winio "github.com/tailscale/go-winio"
	"golang.org/x/sys/windows"
)

// PipeTransport serves on a named pipe (\\.\pipe\…).
type PipeTransport struct {
	// SDDL is the pipe's security descriptor (PipeSDDL). Empty: LocalSystem
	// and Administrators plus the current user (a development helper).
	SDDL string
}

// NewPipeTransport returns a transport with the default descriptor.
func NewPipeTransport() *PipeTransport { return &PipeTransport{} }

// Listen creates the first instance of the pipe; it fails when another
// helper already serves it (FILE_FLAG_FIRST_PIPE_INSTANCE). A pipe cannot
// go stale: it vanishes with its server.
func (t *PipeTransport) Listen(addr string) (net.Listener, error) {
	sddl := t.SDDL
	if sddl == "" {
		self, err := currentSID()
		if err != nil {
			return nil, err
		}
		if sddl, err = PipeSDDL(self); err != nil {
			return nil, err
		}
	}
	l, err := winio.ListenPipe(addr, &winio.PipeConfig{
		SecurityDescriptor: sddl,
		InputBufferSize:    64 << 10,
		OutputBufferSize:   64 << 10,
	})
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_PIPE_BUSY) {
		return nil, fmt.Errorf("%s: another helper is already listening (%w)", addr, err)
	}
	return l, err
}

// Dial connects with PipeClientAccess, the rights the DACL grants.
func (t *PipeTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	return winio.DialPipeAccess(ctx, addr, PipeClientAccess)
}

// PeerOf identifies the client process: its pid from the pipe, then its
// token's user, elevation and Administrators membership.
func (t *PipeTransport) PeerOf(c net.Conn) (Peer, error) {
	f, ok := c.(interface{ Fd() uintptr })
	if !ok {
		return Peer{UID: -1}, errors.New("not a named pipe connection")
	}
	var pid uint32
	if err := windows.GetNamedPipeClientProcessId(windows.Handle(f.Fd()), &pid); err != nil {
		return Peer{UID: -1}, fmt.Errorf("client process id: %w", err)
	}
	return processPeer(pid)
}

func processPeer(pid uint32) (Peer, error) {
	p := Peer{UID: -1, PID: int(pid)}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return p, fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(h)
	var tok windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err != nil {
		return p, fmt.Errorf("token of process %d: %w", pid, err)
	}
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return p, err
	}
	p.SID = u.User.Sid.String()
	p.User = accountName(u.User.Sid)
	p.Elevated = tok.IsElevated()
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return p, err
	}
	if gs, err := tok.GetTokenGroups(); err == nil {
		for _, g := range gs.AllGroups() {
			if g.Attributes&windows.SE_GROUP_ENABLED != 0 && g.Attributes&windows.SE_GROUP_USE_FOR_DENY_ONLY == 0 && windows.EqualSid(g.Sid, admins) {
				p.Admin = true
			}
		}
	}
	return p, nil
}

// accountName is DOMAIN\name of a SID ("" when it has none: a deleted
// account, or a domain account while the domain is unreachable).
func accountName(sid *windows.SID) string {
	return cachedName(sid.String(), func() string { return lookupAccount(sid) })
}

func lookupAccount(sid *windows.SID) string {
	name, domain, _, err := sid.LookupAccount("")
	switch {
	case err != nil || name == "":
		return ""
	case domain == "":
		return name
	}
	return domain + `\` + name
}

func currentSID() (string, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String(), nil
}

// Self is this process's identity in Peer form (logs, status): its
// token's SID and account name, and its pid; never uid -1 in what it
// prints.
func Self() Peer {
	p := Peer{UID: -1, PID: os.Getpid()}
	if u, err := windows.GetCurrentProcessToken().GetTokenUser(); err == nil {
		p.SID = u.User.Sid.String()
		p.User = accountName(u.User.Sid)
	}
	return p
}
