// Package dnswire is a tiny DNS client: one A/AAAA question over UDP,
// enough to ask mihomo's DNS listener what it answers. No dependencies.
package dnswire

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"strings"
	"time"
)

// Record types.
const (
	TypeA    uint16 = 1
	TypeAAAA uint16 = 28
)

var rcodes = map[int]string{0: "NOERROR", 1: "FORMERR", 2: "SERVFAIL", 3: "NXDOMAIN", 4: "NOTIMP", 5: "REFUSED"}

// RcodeName returns the name of a DNS response code.
func RcodeName(c int) string {
	if s, ok := rcodes[c]; ok {
		return s
	}
	return fmt.Sprintf("RCODE%d", c)
}

// Answer is a parsed response.
type Answer struct {
	Rcode string
	Addrs []netip.Addr
}

// BuildQuery returns a DNS query message for name/qtype with recursion desired.
func BuildQuery(id uint16, name string, qtype uint16) ([]byte, error) {
	msg := make([]byte, 12, 64)
	binary.BigEndian.PutUint16(msg[0:], id)
	msg[2] = 0x01 // RD
	binary.BigEndian.PutUint16(msg[4:], 1)
	name = strings.TrimSuffix(name, ".")
	if name != "" {
		for _, label := range strings.Split(name, ".") {
			if len(label) == 0 || len(label) > 63 {
				return nil, fmt.Errorf("dnswire: bad name %q", name)
			}
			msg = append(msg, byte(len(label)))
			msg = append(msg, label...)
		}
	}
	msg = append(msg, 0)
	msg = binary.BigEndian.AppendUint16(msg, qtype)
	msg = binary.BigEndian.AppendUint16(msg, 1) // IN
	return msg, nil
}

// skipName returns the offset after the (possibly compressed) name at off.
func skipName(msg []byte, off int) (int, error) {
	for {
		if off >= len(msg) {
			return 0, errors.New("dnswire: truncated name")
		}
		l := int(msg[off])
		switch {
		case l == 0:
			return off + 1, nil
		case l&0xC0 == 0xC0:
			return off + 2, nil
		default:
			off += 1 + l
		}
	}
}

// ParseResponse parses a response to a query with the given id, returning
// A and AAAA addresses from the answer section.
func ParseResponse(msg []byte, id uint16) (Answer, error) {
	if len(msg) < 12 {
		return Answer{}, errors.New("dnswire: short message")
	}
	if binary.BigEndian.Uint16(msg) != id {
		return Answer{}, errors.New("dnswire: id mismatch")
	}
	if msg[2]&0x80 == 0 {
		return Answer{}, errors.New("dnswire: not a response")
	}
	ans := Answer{Rcode: RcodeName(int(msg[3] & 0x0F))}
	qd := int(binary.BigEndian.Uint16(msg[4:]))
	an := int(binary.BigEndian.Uint16(msg[6:]))
	off := 12
	var err error
	for i := 0; i < qd; i++ {
		if off, err = skipName(msg, off); err != nil {
			return ans, err
		}
		off += 4
	}
	for i := 0; i < an; i++ {
		if off, err = skipName(msg, off); err != nil {
			return ans, err
		}
		if off+10 > len(msg) {
			return ans, errors.New("dnswire: truncated record")
		}
		typ := binary.BigEndian.Uint16(msg[off:])
		rdlen := int(binary.BigEndian.Uint16(msg[off+8:]))
		off += 10
		if off+rdlen > len(msg) {
			return ans, errors.New("dnswire: truncated rdata")
		}
		rd := msg[off : off+rdlen]
		switch {
		case typ == TypeA && rdlen == 4:
			ans.Addrs = append(ans.Addrs, netip.AddrFrom4([4]byte(rd)))
		case typ == TypeAAAA && rdlen == 16:
			ans.Addrs = append(ans.Addrs, netip.AddrFrom16([16]byte(rd)))
		}
		off += rdlen
	}
	return ans, nil
}

// Query sends one question to server ("host:port") over UDP.
func Query(ctx context.Context, server, name string, qtype uint16) (Answer, error) {
	id := uint16(rand.UintN(1 << 16))
	q, err := BuildQuery(id, name, qtype)
	if err != nil {
		return Answer{}, err
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", server)
	if err != nil {
		return Answer{}, err
	}
	defer conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)
	if _, err := conn.Write(q); err != nil {
		return Answer{}, err
	}
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return Answer{}, err
		}
		a, err := ParseResponse(buf[:n], id)
		if err != nil && strings.Contains(err.Error(), "id mismatch") {
			continue
		}
		return a, err
	}
}
