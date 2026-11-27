package foreign

import (
	"bufio"
	"net"
	"strconv"
	"strings"
)

// Port returns the port of a controller address ("127.0.0.1:9090",
// ":9090", "http://127.0.0.1:9090"); 0 if there is none.
func Port(addr string) int {
	a := addr
	if _, rest, ok := strings.Cut(a, "://"); ok {
		a = rest
	}
	a = strings.TrimRight(a, "/")
	_, p, err := net.SplitHostPort(a)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(p)
	if err != nil || n <= 0 || n > 65535 {
		return 0
	}
	return n
}

// ParseLsofPIDs reads the PIDs of `lsof -F p…` output ("p1234" lines).
func ParseLsofPIDs(out string) []int {
	var pids []int
	seen := map[int]bool{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if v, ok := strings.CutPrefix(line, "p"); ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && !seen[n] {
				seen[n] = true
				pids = append(pids, n)
			}
		}
	}
	return pids
}

// ParseLsofName returns the first name ("n…" line) of `lsof -F n` output:
// the cwd of `lsof -a -p PID -d cwd -Fn`.
func ParseLsofName(out string) string {
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "n"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ParsePSPPIDComm reads `ps -o ppid=,comm= -p PID`: the parent PID and the
// executable (the rest of the line, spaces included).
func ParsePSPPIDComm(out string) (ppid int, comm string) {
	line := strings.TrimSpace(firstLine(out))
	f, rest, ok := strings.Cut(line, " ")
	if !ok {
		n, _ := strconv.Atoi(f)
		return n, ""
	}
	n, _ := strconv.Atoi(f)
	return n, strings.TrimSpace(rest)
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

// ProcNetListenInodes reads /proc/net/tcp or tcp6 and returns the socket
// inodes listening on port (state 0A).
func ProcNetListenInodes(table string, port int) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(table))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 10 || f[3] != "0A" {
			continue
		}
		_, hp, ok := strings.Cut(f[1], ":")
		if !ok {
			continue
		}
		p, err := strconv.ParseUint(hp, 16, 32)
		if err != nil || int(p) != port {
			continue
		}
		out = append(out, f[9])
	}
	return out
}

// ProcStatPPID reads the parent PID from /proc/<pid>/stat (the field after
// the state, past the command in parentheses, which may hold spaces).
func ProcStatPPID(stat string) int {
	i := strings.LastIndexByte(stat, ')')
	if i < 0 {
		return 0
	}
	f := strings.Fields(stat[i+1:])
	if len(f) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(f[1])
	return n
}

// ProcCmdline splits /proc/<pid>/cmdline (NUL-separated).
func ProcCmdline(b []byte) []string {
	s := strings.TrimRight(string(b), "\x00")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x00")
}
