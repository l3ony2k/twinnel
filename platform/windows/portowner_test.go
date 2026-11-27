package windows

import (
	"encoding/binary"
	"testing"
)

func TestFindListener(t *testing.T) {
	// MIB_TCPTABLE_OWNER_PID with two rows: 127.0.0.1:7890 (pid 11) and
	// 127.0.0.1:9090 (pid 4242); ports in network byte order.
	b := make([]byte, 4+2*24)
	binary.LittleEndian.PutUint32(b, 2)
	row := func(i, port int, pid uint32) {
		r := b[4+i*24:]
		binary.LittleEndian.PutUint32(r[0:], 2) // LISTEN
		r[8], r[9] = byte(port>>8), byte(port)
		binary.LittleEndian.PutUint32(r[20:], pid)
	}
	row(0, 7890, 11)
	row(1, 9090, 4242)
	if pid := findListener(b, false, 9090); pid != 4242 {
		t.Errorf("v4 pid = %d", pid)
	}
	if pid := findListener(b, false, 9097); pid != 0 {
		t.Errorf("free port pid = %d", pid)
	}
	// MIB_TCP6TABLE_OWNER_PID: one row, [::1]:9090, pid 77.
	b6 := make([]byte, 4+56)
	binary.LittleEndian.PutUint32(b6, 1)
	b6[4+20], b6[4+21] = 0x23, 0x82
	binary.LittleEndian.PutUint32(b6[4+52:], 77)
	if pid := findListener(b6, true, 9090); pid != 77 {
		t.Errorf("v6 pid = %d", pid)
	}
	if pid := findListener(b[:10], false, 9090); pid != 0 {
		t.Errorf("short table pid = %d", pid)
	}
}
