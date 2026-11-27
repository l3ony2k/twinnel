//go:build darwin

package darwin

import (
	"fmt"
	"testing"
)

// `ipconfig getpacket en0` (synthetic addresses).
const dhcpPacket = `op = BOOTREPLY
htype = 1
ciaddr = 192.168.1.20
yiaddr = 192.168.1.20
chaddr = 0:0:5e:0:53:1
options:
Options count is 6
dhcp_message_type (uint8): ACK 0x5
server_identifier (ip): 192.168.1.1
lease_time (uint32): 0x15180
subnet_mask (ip): 255.255.255.0
router (ip_mult): {192.168.1.1}
domain_name_server (ip_mult): {192.168.1.1, 192.0.2.53}
end (none):
`

func TestParseDHCPPacket(t *testing.T) {
	dns, routers := ParseDHCPPacket(dhcpPacket)
	if fmt.Sprint(dns) != "[192.168.1.1 192.0.2.53]" || fmt.Sprint(routers) != "[192.168.1.1]" {
		t.Fatalf("dns %v routers %v", dns, routers)
	}
	if dns, routers := ParseDHCPPacket(""); dns != nil || routers != nil {
		t.Fatal("empty packet")
	}
}
