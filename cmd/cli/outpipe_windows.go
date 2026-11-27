//go:build windows

package main

import (
	"errors"
	"io"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The elevated copy's output pipe (--output). One instance, made before
// the elevated copy starts: the copy can connect from its first
// instruction. (go-winio's ListenPipe is not used here: its first instance
// is created without read or write access and refuses clients with
// ERROR_PIPE_BUSY until Accept has made another one, so a copy that
// connects before Accept runs gets PIPE_BUSY.)

// outputPipe is the server end of one --output pipe.
type outputPipe struct {
	name string
	h    windows.Handle
}

// listenOutput creates the pipe's only instance, inbound, local clients
// only, with the security descriptor sddl. It fails when the name exists
// (FILE_FLAG_FIRST_PIPE_INSTANCE): nobody can squat it first.
func listenOutput(name, sddl string) (*outputPipe, error) {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return nil, err
	}
	sa := &windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(*sa))
	n16, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateNamedPipe(n16,
		windows.PIPE_ACCESS_INBOUND|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		1, 0, 64<<10, 0, sa)
	if err != nil {
		return nil, err
	}
	return &outputPipe{name: name, h: h}, nil
}

// copyTo waits for the client, copies what it writes to w until it closes
// its end, and closes the pipe. A client that connected before copyTo was
// called is taken as connected (ERROR_PIPE_CONNECTED).
func (p *outputPipe) copyTo(w io.Writer) {
	defer windows.CloseHandle(p.h)
	err := windows.ConnectNamedPipe(p.h, nil)
	if err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) && !errors.Is(err, windows.ERROR_NO_DATA) {
		return
	}
	buf := make([]byte, 4096)
	for {
		var n uint32
		err := windows.ReadFile(p.h, buf, &n, nil)
		if n > 0 {
			w.Write(buf[:n])
		}
		if err != nil { // ERROR_BROKEN_PIPE: the client closed its end
			return
		}
	}
}

// release ends a copyTo whose client never came (the prompt was
// dismissed, or the elevated copy failed before connecting): it connects
// and hangs up itself, so ConnectNamedPipe returns and the read sees the
// end. When the client is connected, the one instance is busy and this
// does nothing.
func (p *outputPipe) release() {
	if f, err := os.OpenFile(p.name, os.O_WRONLY, 0); err == nil {
		f.Close()
	}
}

// openOutputPipe is the client end, for the elevated copy. The pipe can
// be busy for a moment or (with a server that makes its instance late)
// not there yet: it retries until the deadline.
func openOutputPipe(name string, wait time.Duration) (*os.File, error) {
	deadline := time.Now().Add(wait)
	for {
		f, err := os.OpenFile(name, os.O_WRONLY, 0)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}
