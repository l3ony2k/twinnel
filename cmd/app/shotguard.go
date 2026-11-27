package main

import (
	"errors"
	"net"

	"github.com/l3ony2k/duet/core/rpc"
)

// shotGuard refuses a shot run (DUET_SHOTS set) that could reach the
// owner's live stack: it must name a scratch helper socket (DUET_SOCKET,
// never the installed helper's) and a fake controller (DUET_CONTROLLER,
// never port 9090). A normal run passes.
func shotGuard(getenv func(string) string) error {
	if getenv("DUET_SHOTS") == "" {
		return nil
	}
	if s := getenv("DUET_SOCKET"); s == "" || s == rpc.DefaultSocket {
		return errors.New("shots need DUET_SOCKET set to a scratch helper's socket, not the installed helper's")
	}
	c := getenv("DUET_CONTROLLER")
	if c == "" {
		return errors.New("shots need DUET_CONTROLLER set to a fake mihomo controller")
	}
	if _, port, err := net.SplitHostPort(c); err != nil || port == "9090" {
		return errors.New("shots refuse DUET_CONTROLLER " + c + ": port 9090 is the live mihomo")
	}
	return nil
}
