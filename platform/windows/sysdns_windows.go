//go:build windows

package windows

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// NewStaticDNS reads hand-set DNS servers from the registry (read-only).
func NewStaticDNS() StaticDNS { return regStaticDNS{} }

type regStaticDNS struct{}

const (
	tcpipInterfaces  = `SYSTEM\CurrentControlSet\Services\Tcpip\Parameters\Interfaces\`
	tcpip6Interfaces = `SYSTEM\CurrentControlSet\Services\Tcpip6\Parameters\Interfaces\`
)

func (regStaticDNS) StaticDNS(guid string) (v4, v6 []string, err error) {
	if guid == "" {
		return nil, nil, nil
	}
	if v4, err = readNameServer(tcpipInterfaces + guid); err != nil {
		return nil, nil, err
	}
	if v6, err = readNameServer(tcpip6Interfaces + guid); err != nil {
		return nil, nil, err
	}
	return v4, v6, nil
}

// readNameServer reads NameServer under path; a missing key or value is
// no hand-set servers.
func readNameServer(path string) ([]string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer k.Close()
	s, _, err := k.GetStringValue("NameServer")
	if errors.Is(err, registry.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseNameServer(s), nil
}
