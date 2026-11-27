package main

import (
	"github.com/l3ony2k/twinnel/core/binsane"
	"github.com/l3ony2k/twinnel/platform/windows/winsec"
)

// Installed binaries' signatures are checked with Authenticode.
func init() { binsane.VerifySignature = winsec.VerifyAuthenticode }
