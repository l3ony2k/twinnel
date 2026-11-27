// Command fakemihomo is a development stand-in for mihomo's external
// controller with synthetic data: groups and nodes with delays, a live
// /traffic and /logs stream, /connections, and writes that change only its
// own memory. The app's screenshots run against it so no page shows the
// owner's real nodes, and nothing touches the live mihomo.
//
//	go run ./cmd/app/internal/fakemihomo --listen 127.0.0.1:19290
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type proxy struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Alive   bool     `json:"alive"`
	UDP     bool     `json:"udp"`
	Now     string   `json:"now,omitempty"`
	All     []string `json:"all,omitempty"`
	History []rec    `json:"history"`
}

type rec struct {
	Time  time.Time `json:"time"`
	Delay int       `json:"delay"`
}

type server struct {
	mu      sync.Mutex
	proxies map[string]*proxy
	mode    string
	tun     bool
	start   time.Time
	down    int64
	up      int64
}

// Synthetic nodes: plausible names, nothing from a real subscription.
var nodes = []struct {
	name, typ string
	delay     int
}{
	{"香港 01", "Trojan", 46}, {"香港 02", "Trojan", 52}, {"香港 03 IPLC", "Shadowsocks", 31},
	{"日本 东京 01", "Vless", 88}, {"日本 大阪 02", "Vless", 104},
	{"新加坡 01", "Trojan", 71}, {"新加坡 02", "Trojan", 0},
	{"美国 洛杉矶 01", "Hysteria2", 162}, {"美国 圣何塞 02", "Hysteria2", 175},
	{"台湾 01", "Shadowsocks", 63}, {"英国 伦敦 01", "Vless", 231}, {"德国 法兰克福", "Vless", 0},
}

// enNames renames the synthetic nodes and groups for --lang en (English
// screenshots, the README's).
var enNames = map[string]string{
	"香港 01": "Hong Kong 01", "香港 02": "Hong Kong 02", "香港 03 IPLC": "Hong Kong 03 IPLC",
	"日本 东京 01": "Tokyo 01", "日本 大阪 02": "Osaka 02", "新加坡 01": "Singapore 01", "新加坡 02": "Singapore 02",
	"美国 洛杉矶 01": "US West 01", "美国 圣何塞 02": "US West 02", "台湾 01": "Taipei 01",
	"英国 伦敦 01": "London 01", "德国 法兰克福": "Frankfurt 01",
	"自动选择": "Auto", "香港": "Hong Kong", "漏网之鱼": "Final",
}

// useEnglish switches every synthetic name to its English one.
func useEnglish() {
	for i := range nodes {
		nodes[i].name = enName(nodes[i].name)
	}
	for i := range logLines {
		m := logLines[i].msg
		for zh, en := range enNames {
			m = strings.ReplaceAll(m, zh+"]", en+"]")
			m = strings.ReplaceAll(m, "dial "+zh+" ", "dial "+en+" ")
		}
		logLines[i].msg = m
	}
	english = true
}

var english bool

func enName(s string) string {
	if e, ok := enNames[s]; ok {
		return e
	}
	return s
}

// n is a synthetic name in the run's language.
func n(s string) string {
	if english {
		return enName(s)
	}
	return s
}

func newServer() *server {
	s := &server{proxies: map[string]*proxy{}, mode: "rule", tun: true, start: time.Now()}
	now := time.Now()
	var names []string
	for _, n := range nodes {
		s.proxies[n.name] = &proxy{Name: n.name, Type: n.typ, Alive: n.delay > 0, UDP: true,
			History: []rec{{Time: now.Add(-3 * time.Minute), Delay: n.delay}}}
		names = append(names, n.name)
	}
	for _, b := range []string{"DIRECT", "REJECT"} {
		s.proxies[b] = &proxy{Name: b, Type: map[string]string{"DIRECT": "Direct", "REJECT": "Reject"}[b], Alive: true, UDP: true}
	}
	group := func(name, typ, now string, all []string) {
		s.proxies[name] = &proxy{Name: name, Type: typ, Alive: true, UDP: true, Now: now, All: all}
	}
	hk := []string{n("香港 01"), n("香港 02"), n("香港 03 IPLC")}
	group(n("自动选择"), "URLTest", n("香港 03 IPLC"), names)
	group(n("香港"), "URLTest", n("香港 01"), hk)
	group("PROXY", "Selector", n("自动选择"), append(append([]string{n("自动选择"), n("香港"), "DIRECT"}, names...), "REJECT"))
	group("OpenAI", "Selector", n("美国 洛杉矶 01"), []string{n("美国 洛杉矶 01"), n("美国 圣何塞 02"), n("日本 东京 01"), "PROXY"})
	group("Apple", "Selector", "DIRECT", []string{"DIRECT", "PROXY", n("香港")})
	group("Telegram", "Selector", n("新加坡 01"), []string{n("新加坡 01"), n("新加坡 02"), "PROXY"})
	group(n("漏网之鱼"), "Selector", "PROXY", []string{"PROXY", "DIRECT"})
	order := []string{"PROXY", n("自动选择"), n("香港"), "OpenAI", "Apple", "Telegram", n("漏网之鱼")}
	group("GLOBAL", "Selector", "PROXY", append(order, "DIRECT", "REJECT"))
	// The groups' own delay is their selection's.
	for _, g := range order {
		s.proxies[g].History = s.resolveHistory(g)
	}
	return s
}

func (s *server) resolveHistory(name string) []rec {
	seen := map[string]bool{}
	for p := s.proxies[name]; p != nil && !seen[p.Name]; p = s.proxies[p.Now] {
		seen[p.Name] = true
		if p.Now == "" {
			return p.History
		}
	}
	return nil
}

func (s *server) json(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// rate is a smooth synthetic traffic curve with some noise.
func (s *server) rate() (down, up int64) {
	t := time.Since(s.start).Seconds()
	d := 380e3 + 260e3*math.Sin(t/9) + 180e3*math.Sin(t/3.1) + rand.Float64()*90e3
	u := 48e3 + 30e3*math.Sin(t/7+1) + rand.Float64()*20e3
	return int64(max(d, 12e3)), int64(max(u, 4e3))
}

var logLines = []struct{ typ, msg string }{
	{"info", "[TCP] 192.168.1.20:52144 --> api.github.com:443 match DomainSuffix(github.com) using PROXY[香港 03 IPLC]"},
	{"info", "[TCP] 192.168.1.20:52150 --> www.apple.com:443 match RuleSet(apple) using Apple[DIRECT]"},
	{"info", "[UDP] 192.168.1.20:61022 --> 1.1.1.1:443 match GeoIP(!CN) using PROXY[香港 03 IPLC]"},
	{"info", "[TCP] 192.168.1.20:52163 --> chat.openai.com:443 match RuleSet(openai) using OpenAI[美国 洛杉矶 01]"},
	{"warning", "[TCP] dial 新加坡 02 (match RuleSet(telegram)) 192.168.1.20:52170 --> 149.154.167.51:443 error: i/o timeout"},
	{"info", "[TCP] 192.168.1.20:52181 --> mirrors.tuna.tsinghua.edu.cn:443 match GeoSite(cn) using DIRECT"},
	{"debug", "[DNS] resolve example.org A from https://1.1.1.1/dns-query"},
	{"info", "[TCP] 100.64.0.2:50221 --> 100.64.0.7:22 match IPCIDR(100.64.0.0/10) using DIRECT"},
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		s.json(w, map[string]any{"meta": true, "version": "v1.19.34"})
	})
	mux.HandleFunc("GET /configs", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.json(w, map[string]any{"mode": s.mode, "mixed-port": 7890, "log-level": "info", "ipv6": false,
			"tun": map[string]any{"enable": s.tun, "device": "utun1024", "stack": "gvisor", "auto-route": true, "mtu": 9000}})
	})
	mux.HandleFunc("PATCH /configs", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Mode string `json:"mode"`
			Tun  *struct {
				Enable bool `json:"enable"`
			} `json:"tun"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		if body.Mode != "" {
			s.mode = body.Mode
		}
		if body.Tun != nil {
			s.tun = body.Tun.Enable
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	// A config holding the marker duet-test-reload-fail is refused, as
	// mihomo refuses one it cannot load, so the app's failed-reload state
	// can be shown (shots, design review U53).
	mux.HandleFunc("PUT /configs", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path string `json:"path"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if b, err := os.ReadFile(body.Path); err == nil && strings.Contains(string(b), "duet-test-reload-fail") {
			http.Error(w, `{"message":"synthetic reload failure (duet-test-reload-fail)"}`, http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /cache/fakeip/flush", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("POST /cache/dns/flush", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /proxies", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.json(w, map[string]any{"proxies": s.proxies})
	})
	mux.HandleFunc("GET /proxies/{name}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		p := s.proxies[r.PathValue("name")]
		if p == nil {
			http.NotFound(w, r)
			return
		}
		s.json(w, p)
	})
	mux.HandleFunc("PUT /proxies/{name}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		defer s.mu.Unlock()
		if p := s.proxies[r.PathValue("name")]; p != nil && p.Type == "Selector" {
			p.Now = body.Name
			p.History = s.resolveHistory(p.Name)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	delay := func(name string) int {
		p := s.proxies[name]
		if p == nil {
			return 0
		}
		for _, n := range nodes {
			if n.name == name && n.delay > 0 {
				d := n.delay + rand.IntN(20) - 10
				p.History = append(p.History, rec{Time: time.Now(), Delay: d})
				return d
			}
		}
		return 0
	}
	mux.HandleFunc("GET /proxies/{name}/delay", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("delay test: %s", r.PathValue("name")) // lets a run check each node is hit once
		s.mu.Lock()
		d := delay(r.PathValue("name"))
		s.mu.Unlock()
		time.Sleep(300 * time.Millisecond)
		if d == 0 {
			http.Error(w, `{"message":"Timeout"}`, http.StatusGatewayTimeout)
			return
		}
		s.json(w, map[string]int{"delay": d})
	})
	mux.HandleFunc("GET /group/{name}/delay", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(900 * time.Millisecond)
		s.mu.Lock()
		defer s.mu.Unlock()
		g := s.proxies[r.PathValue("name")]
		if g == nil {
			http.NotFound(w, r)
			return
		}
		out := map[string]int{}
		for _, m := range g.All {
			if d := delay(m); d > 0 {
				out[m] = d
			}
		}
		g.History = s.resolveHistory(g.Name)
		s.json(w, out)
	})
	mux.HandleFunc("GET /connections", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		down, up := s.down, s.up
		s.mu.Unlock()
		conns := make([]map[string]any, 37)
		for i := range conns {
			conns[i] = map[string]any{"id": fmt.Sprint(i), "chains": []string{n("香港 03 IPLC"), "PROXY"}, "start": time.Now()}
		}
		s.json(w, map[string]any{"downloadTotal": down, "uploadTotal": up, "connections": conns, "memory": 48 << 20})
	})
	stream := func(w http.ResponseWriter, r *http.Request, every time.Duration, line func(i int) any) {
		fl, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/json")
		t := time.NewTicker(every)
		defer t.Stop()
		for i := 0; ; i++ {
			b, _ := json.Marshal(line(i))
			if _, err := w.Write(append(b, '\n')); err != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
			select {
			case <-r.Context().Done():
				return
			case <-t.C:
			}
		}
	}
	mux.HandleFunc("GET /traffic", func(w http.ResponseWriter, r *http.Request) {
		stream(w, r, time.Second, func(int) any {
			d, u := s.rate()
			s.mu.Lock()
			s.down += d
			s.up += u
			// Start the totals at a plausible day's worth.
			defer s.mu.Unlock()
			return map[string]int64{"down": d, "up": u, "downTotal": s.down + 3_870_000_000, "upTotal": s.up + 412_000_000}
		})
	})
	mux.HandleFunc("GET /logs", func(w http.ResponseWriter, r *http.Request) {
		stream(w, r, 400*time.Millisecond, func(i int) any {
			l := logLines[i%len(logLines)]
			return map[string]string{"type": l.typ, "payload": l.msg}
		})
	})
	return mux
}

func main() {
	listen := flag.String("listen", "127.0.0.1:19290", "controller address")
	lang := flag.String("lang", "zh", "node and group names: zh or en")
	flag.Parse()
	if *lang == "en" {
		useEnglish()
	}
	s := newServer()
	h := s.handler()
	fmt.Println("fake mihomo controller on", *listen)
	log.Fatal(http.ListenAndServe(*listen, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/traffic" && r.URL.Path != "/logs" {
			log.Printf("%s %s", r.Method, r.URL)
		}
		h.ServeHTTP(w, r)
	})))
}
