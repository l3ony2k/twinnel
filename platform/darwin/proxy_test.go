//go:build darwin

package darwin

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestParseScutilProxy(t *testing.T) {
	on := `<dictionary> {
  ExceptionsList : <array> {
    0 : 127.0.0.1
    1 : localhost
  }
  FTPPassive : 1
  HTTPEnable : 1
  HTTPPort : 7890
  HTTPProxy : 127.0.0.1
  HTTPSEnable : 1
  HTTPSPort : 7891
  HTTPSProxy : 127.0.0.1
  SOCKSEnable : 1
  SOCKSPort : 7892
  SOCKSProxy : 127.0.0.1
}`
	if u := ParseScutilProxy(on); u == nil || u.String() != "http://127.0.0.1:7891" {
		t.Errorf("https: %v", u)
	}
	socks := `<dictionary> {
  HTTPSEnable : 0
  SOCKSEnable : 1
  SOCKSPort : 1080
  SOCKSProxy : 10.0.0.2
}`
	if u := ParseScutilProxy(socks); u == nil || u.String() != "socks5://10.0.0.2:1080" {
		t.Errorf("socks: %v", u)
	}
	if u := ParseScutilProxy("<dictionary> {\n  HTTPEnable : 0\n}"); u != nil {
		t.Errorf("off: %v", u)
	}
	run := func(context.Context, string, ...string) ([]byte, error) { return []byte(on), nil }
	r, _ := http.NewRequest("GET", "https://api.github.com/", nil)
	if u, err := SystemProxy(run)(r); err != nil || u.Host != "127.0.0.1:7891" {
		t.Errorf("system proxy %v %v", u, err)
	}
	fail := func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("no scutil") }
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	if u, err := SystemProxy(fail)(r); err != nil || u != nil {
		t.Errorf("fallback %v %v", u, err)
	}
}
