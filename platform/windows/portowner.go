package windows

import "encoding/binary"

// findListener walks a MIB_TCPTABLE_OWNER_PID (24-byte rows: state, local
// addr, local port, remote addr, remote port, pid) or a
// MIB_TCP6TABLE_OWNER_PID (56-byte rows: local addr[16], scope, local
// port, remote addr[16], scope, remote port, state, pid).
func findListener(b []byte, v6 bool, port int) uint32 {
	if len(b) < 4 {
		return 0
	}
	n := int(binary.LittleEndian.Uint32(b))
	row, portOff, pidOff := 24, 8, 20
	if v6 {
		row, portOff, pidOff = 56, 20, 52
	}
	for i := 0; i < n; i++ {
		off := 4 + i*row
		if off+row > len(b) {
			break
		}
		r := b[off : off+row]
		// The port is in network byte order in the low 16 bits.
		lp := int(r[portOff])<<8 | int(r[portOff+1])
		if lp == port {
			return binary.LittleEndian.Uint32(r[pidOff:])
		}
	}
	return 0
}
