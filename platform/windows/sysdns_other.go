//go:build !windows

package windows

// NewStaticDNS returns a StaticDNS that fails: the values live in the
// Windows registry. Tests pass their own.
func NewStaticDNS() StaticDNS { return noStaticDNS{} }

type noStaticDNS struct{}

func (noStaticDNS) StaticDNS(string) (v4, v6 []string, err error) { return nil, nil, errNoSys() }
