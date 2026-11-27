//go:build darwin

package main

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"

	"github.com/l3ony2k/duet/platform/darwin"
)

// macOSMajor is the running macOS's major version (26 for Tahoe), or 0
// when it cannot be read.
func macOSMajor() int {
	v, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.SplitN(v, ".", 2)[0])
	return n
}

// liquidGlass says whether the system draws Liquid Glass (macOS 26 and
// later): toolbar items then sit in glass capsules.
var liquidGlass = macOSMajor() >= 26

// systemProxy is the update check's proxy: the system's settings (scutil
// --proxy), then the environment.
func systemProxy() func(*http.Request) (*url.URL, error) { return darwin.SystemProxy(nil) }
