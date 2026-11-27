// Command fakeexe stands in for Twinnel's programs in install_windows_test:
// `version --short` prints the version it was built with (install.ps1
// writes it into the marker); anything else keeps running, holding its
// file the way an agent's `twinnel mcp` does, until it is stopped (at most
// ten minutes).
package main

import (
	"fmt"
	"os"
	"time"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(version)
		return
	}
	time.Sleep(10 * time.Minute)
}
