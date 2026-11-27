// Command devfake is a development stand-in for mihomo: it serves a fake
// external-controller (GET /version, PUT /configs) and writes a fake
// `mihomo` script for `mihomo -t`. Use it to run the helper against
// something that is not the live mihomo.
//
//	go run ./cmd/helper/internal/devfake --listen 127.0.0.1:19090 --secret dev --write-mihomo /tmp/x/bin
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/l3ony2k/twinnel/core/engine/enginetest"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:19090", "controller address")
	secret := flag.String("secret", "", "controller secret")
	binDir := flag.String("write-mihomo", "", "write the fake mihomo script into `DIR`")
	flag.Parse()
	if *binDir != "" {
		if err := os.MkdirAll(*binDir, 0o755); err != nil {
			log.Fatal(err)
		}
		p, err := enginetest.WriteFakeMihomo(*binDir)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("fake mihomo:", p)
	}
	c := enginetest.NewController(*secret)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("controller: %s %s", r.Method, r.URL)
		c.ServeHTTP(w, r)
	})
	fmt.Println("fake controller on", *listen)
	log.Fatal(http.ListenAndServe(*listen, h))
}
