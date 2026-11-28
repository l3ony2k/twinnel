package engine

import (
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/l3ony2k/twinnel/core/health"
)

// dnsModeCache is the DNS mode of the mihomo config file for status.get,
// read again only when the file changed (its size or time): status.get is
// asked every few seconds, and a config can be large.
type dnsModeCache struct {
	mu   sync.Mutex
	path string
	size int64
	mod  time.Time
	mode string
}

// of is the DNS mode of the config file at path (health's Mode), "" when
// it cannot be read or parsed.
func (c *dnsModeCache) of(path string) string {
	if path == "" {
		return ""
	}
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == path && c.size == fi.Size() && c.mod.Equal(fi.ModTime()) {
		return c.mode
	}
	mode := ""
	if b, err := os.ReadFile(path); err == nil {
		var f struct {
			DNS struct {
				Enable       bool   `yaml:"enable"`
				EnhancedMode string `yaml:"enhanced-mode"`
			} `yaml:"dns"`
		}
		if yaml.Unmarshal(b, &f) == nil {
			mode = health.MihomoDNSConfig{Enable: f.DNS.Enable, EnhancedMode: f.DNS.EnhancedMode}.Mode()
		}
	}
	c.path, c.size, c.mod, c.mode = path, fi.Size(), fi.ModTime(), mode
	return mode
}
