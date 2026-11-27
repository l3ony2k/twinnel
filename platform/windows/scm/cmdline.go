package scm

import (
	"fmt"
	"strings"
)

// EscapeArg quotes one argument the way the Microsoft C runtime (and
// CommandLineToArgvW) parses it back; the same algorithm as Go's
// syscall.EscapeArg, which only exists on Windows. No shell is involved:
// the result goes into a service's command line or ShellExecuteEx's
// parameters, never to cmd.exe.
func EscapeArg(s string) string {
	if s == "" {
		return `""`
	}
	needsBackslash, hasSpace := false, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\\':
			needsBackslash = true
		case ' ', '\t':
			hasSpace = true
		}
	}
	if !needsBackslash && !hasSpace {
		return s
	}
	if !needsBackslash {
		return `"` + s + `"`
	}
	var b strings.Builder
	if hasSpace {
		b.WriteByte('"')
	}
	slashes := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		default:
			slashes = 0
		case '\\':
			slashes++
		case '"':
			for ; slashes > 0; slashes-- {
				b.WriteByte('\\')
			}
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	if hasSpace {
		for ; slashes > 0; slashes-- {
			b.WriteByte('\\')
		}
		b.WriteByte('"')
	}
	return b.String()
}

// CommandLine joins argv into one command line with EscapeArg. Arguments
// with control characters (NUL, newlines, …) are refused: they cannot be
// passed safely.
func CommandLine(argv []string) (string, error) {
	if len(argv) == 0 {
		return "", fmt.Errorf("empty command line")
	}
	q := make([]string, len(argv))
	for i, a := range argv {
		if strings.ContainsFunc(a, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return "", fmt.Errorf("refusing argument with control characters: %q", a)
		}
		q[i] = EscapeArg(a)
	}
	return strings.Join(q, " "), nil
}

// SplitCommandLine parses a command line with the rules of
// CommandLineToArgvW for arguments after the first (2n backslashes + quote
// → n backslashes and a quote toggle; 2n+1 → n backslashes and a literal
// quote; "" inside quotes → a literal quote). Used to check CommandLine
// round-trips, and to read a service's command line back.
func SplitCommandLine(s string) []string {
	var (
		args    []string
		cur     strings.Builder
		inQuote bool
		have    bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\':
			n := 0
			for i < len(s) && s[i] == '\\' {
				n++
				i++
			}
			if i < len(s) && s[i] == '"' {
				cur.WriteString(strings.Repeat(`\`, n/2))
				if n%2 == 1 {
					cur.WriteByte('"')
				} else {
					inQuote = !inQuote
				}
				have = true
			} else {
				cur.WriteString(strings.Repeat(`\`, n))
				i--
				have = true
			}
		case c == '"':
			if inQuote && i+1 < len(s) && s[i+1] == '"' {
				cur.WriteByte('"')
				i++
			} else {
				inQuote = !inQuote
			}
			have = true
		case (c == ' ' || c == '\t') && !inQuote:
			if have {
				args = append(args, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteByte(c)
			have = true
		}
	}
	if have {
		args = append(args, cur.String())
	}
	return args
}
