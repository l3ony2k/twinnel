// Package binsane tells whether an installed executable is sane: present,
// readable, not empty or zeroed out, with the executable header of its
// platform (PE "MZ" on Windows, Mach-O on macOS, ELF on Linux) and, where
// the platform signs it, a valid signature (Windows Authenticode, through
// a hook the platform code sets).
//
// It exists because an interrupted installer can leave a file of the
// right size whose bytes are zeros: the service binary next to it runs,
// so nothing else notices until the CLI is run (see
// docs/windows-takeover.md).
package binsane

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// Problem is what is wrong with a binary ("" for nothing).
type Problem string

const (
	OK        Problem = ""
	Missing   Problem = "missing"
	Denied    Problem = "denied" // this process may not read it (not a fault of the file)
	Empty     Problem = "empty"
	Zeroed    Problem = "zeroed"     // the head is all zero bytes
	BadHeader Problem = "bad-header" // not an executable of this platform
	BadSign   Problem = "bad-signature"
)

// Damaged reports whether p means the file must be reinstalled. Missing
// and Denied are not damage: one is not installed, the other unreadable
// from here.
func (p Problem) Damaged() bool {
	return p == Empty || p == Zeroed || p == BadHeader || p == BadSign
}

// Result is one binary's check.
type Result struct {
	Path    string  `json:"path"`
	Problem Problem `json:"problem,omitempty"`
	Detail  string  `json:"detail,omitempty"` // English: the header seen, the signature error
}

// VerifySignature checks a file's platform signature (Windows: Authenticode
// through WinVerifyTrust). nil where there is none to check; platform code
// sets it.
var VerifySignature func(path string) error

// headLen is how much of the file is read.
const headLen = 4096

// Check checks path as an executable of goos. signed asks for a valid
// signature too (only where VerifySignature is set).
func Check(path, goos string, signed bool) Result {
	r := Result{Path: path}
	f, err := os.Open(path)
	if err != nil {
		r.Problem, r.Detail = openProblem(err), err.Error()
		return r
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		r.Problem, r.Detail = openProblem(err), err.Error()
		return r
	}
	if fi.Size() == 0 {
		r.Problem = Empty
		return r
	}
	head := make([]byte, headLen)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		r.Problem, r.Detail = openProblem(err), err.Error()
		return r
	}
	head = head[:n]
	if allZero(head) {
		r.Problem, r.Detail = Zeroed, fmt.Sprintf("the first %d bytes are zero", n)
		return r
	}
	if !HeaderOK(head, goos) {
		r.Problem, r.Detail = BadHeader, fmt.Sprintf("starts with % x", head[:min(4, len(head))])
		return r
	}
	if signed && VerifySignature != nil {
		if err := VerifySignature(path); err != nil {
			r.Problem, r.Detail = BadSign, err.Error()
		}
	}
	return r
}

// openProblem tells "not there" from "not allowed to read".
func openProblem(err error) Problem {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Missing
	case errors.Is(err, fs.ErrPermission):
		return Denied
	}
	return Denied
}

func allZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

// HeaderOK reports whether head starts like an executable of goos (a
// "#!" script counts on Unix).
func HeaderOK(head []byte, goos string) bool {
	if len(head) < 4 {
		return false
	}
	if goos != "windows" && head[0] == '#' && head[1] == '!' {
		return true // a script is an executable on Unix
	}
	switch goos {
	case "windows":
		return head[0] == 'M' && head[1] == 'Z'
	case "darwin":
		m := binary.BigEndian.Uint32(head)
		switch m {
		case 0xfeedface, 0xfeedfacf, 0xcefaedfe, 0xcffaedfe, // Mach-O 32/64, both byte orders
			0xcafebabe, 0xbebafeca: // universal
			return true
		}
		return false
	case "linux", "freebsd", "openbsd", "netbsd":
		return bytes.HasPrefix(head, []byte("\x7fELF"))
	}
	return true
}
