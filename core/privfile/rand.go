package privfile

import (
	"crypto/rand"
	"encoding/hex"
)

func randSuffix() string {
	var b [6]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
