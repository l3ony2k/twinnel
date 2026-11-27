package main

import (
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
)

func TestServiceHostLine(t *testing.T) {
	for _, tc := range []struct {
		h    rpc.ServiceHost
		want []string
		not  []string
	}{
		{h: rpc.ServiceHost{Path: `C:\T\twinnel-svchost.exe`, Version: "0.2.0"}, want: []string{"0.2.0", `C:\T\twinnel-svchost.exe`}, not: []string{"waiting", "out of date"}},
		{h: rpc.ServiceHost{Path: `C:\T\h.exe`, Aside: 1}, want: []string{"1 old copy waiting", "mihomo restarts"}},
		{h: rpc.ServiceHost{Path: `C:\T\h.exe`, Aside: 2, Stale: true}, want: []string{"2 old copies waiting", "twinnel helper upgrade"}},
	} {
		got := serviceHostLine(tc.h)
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%q lacks %q", got, w)
			}
		}
		for _, n := range tc.not {
			if strings.Contains(got, n) {
				t.Errorf("%q has %q", got, n)
			}
		}
	}
}
