package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
)

// fakeHelperRPC answers helperRPC from a map of method → result (nil: the
// helper is down). It records the methods asked.
func fakeHelperRPC(t *testing.T, answers map[string]any) *[]string {
	t.Helper()
	var asked []string
	old := helperRPC
	helperRPC = func(_ context.Context, _ string, method string, _, result any) error {
		asked = append(asked, method)
		if answers == nil {
			return errors.New("cannot reach the helper at /nonexistent: no such file")
		}
		a, ok := answers[method]
		if !ok {
			return &rpc.Error{Code: rpc.CodeMethodNotFound, Message: "method not found"}
		}
		b, _ := json.Marshal(a)
		return json.Unmarshal(b, result)
	}
	t.Cleanup(func() { helperRPC = old })
	return &asked
}

// fakeAppConfig points appConfigPath at an app.json in a temp dir.
func fakeAppConfig(t *testing.T, cfg string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app.json")
	if cfg != "" {
		if err := os.WriteFile(p, []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := appConfigPath
	appConfigPath = func() string { return p }
	t.Cleanup(func() { appConfigPath = old })
}

func clearControllerEnv(t *testing.T) {
	t.Setenv(status.EnvController, "")
	t.Setenv(status.EnvSecret, "")
}

func TestControllerForHelperWins(t *testing.T) {
	clearControllerEnv(t)
	fakeAppConfig(t, `{"controller":"127.0.0.1:9090","secret":"stale-typed"}`)
	fakeHelperRPC(t, map[string]any{rpc.MethodControllerCredentials: rpc.ControllerCredentials{Controller: "127.0.0.1:19190", Secret: "from-helper"}})
	got := controllerFor(context.Background(), "/x.sock", "")
	if got.Source != controllerFromHelper || got.Controller != "127.0.0.1:19190" || got.Secret != "from-helper" {
		t.Fatalf("got %+v", got)
	}
	// --controller changes the address only.
	got = controllerFor(context.Background(), "/x.sock", "127.0.0.1:29090")
	if got.Controller != "127.0.0.1:29090" || got.Secret != "from-helper" {
		t.Fatalf("flag: %+v", got)
	}
	t.Setenv(status.EnvSecret, "env-secret")
	if got = controllerFor(context.Background(), "/x.sock", "127.0.0.1:29090"); got.Secret != "env-secret" {
		t.Fatalf("flag + env secret: %+v", got)
	}
}

// A helper without a secret (an open controller) clears app.json's: the
// helper knows what mihomo loaded.
func TestControllerForHelperEmptySecret(t *testing.T) {
	clearControllerEnv(t)
	fakeAppConfig(t, `{"secret":"stale-typed"}`)
	fakeHelperRPC(t, map[string]any{rpc.MethodControllerCredentials: rpc.ControllerCredentials{Controller: "127.0.0.1:9090"}})
	if got := controllerFor(context.Background(), "/x.sock", ""); got.Secret != "" || got.Source != controllerFromHelper {
		t.Fatalf("got %+v", got)
	}
}

func TestControllerForNoHelper(t *testing.T) {
	clearControllerEnv(t)
	fakeAppConfig(t, `{"controller":"127.0.0.1:19090","secret":"typed"}`)
	fakeHelperRPC(t, nil)
	got := controllerFor(context.Background(), "/x.sock", "")
	if got.Source != controllerFromApp || got.Controller != "127.0.0.1:19090" || got.Secret != "typed" {
		t.Fatalf("got %+v", got)
	}
	fakeAppConfig(t, "")
	if got = controllerFor(context.Background(), "/x.sock", ""); got.Controller != "127.0.0.1:9090" || got.Secret != "" {
		t.Fatalf("no app.json: %+v", got)
	}
}

// TWINNEL_CONTROLLER (development) pins the controller: the helper is not
// even asked, so a scratch helper's default settings never send a dev run
// to the live mihomo.
func TestControllerForEnvPins(t *testing.T) {
	t.Setenv(status.EnvController, "127.0.0.1:59999")
	t.Setenv(status.EnvSecret, "dev")
	fakeAppConfig(t, "")
	asked := fakeHelperRPC(t, map[string]any{rpc.MethodControllerCredentials: rpc.ControllerCredentials{Controller: "127.0.0.1:9090", Secret: "live"}})
	got := controllerFor(context.Background(), "/x.sock", "")
	if got.Source != controllerFromEnv || got.Controller != "127.0.0.1:59999" || got.Secret != "dev" || len(*asked) != 0 {
		t.Fatalf("got %+v, asked %v", got, *asked)
	}
}

func TestHealthSettingsFromHelper(t *testing.T) {
	old := doctorBinaries
	doctorBinaries = func() []health.BinaryRef { return nil }
	t.Cleanup(func() { doctorBinaries = old })

	fakeHelperRPC(t, map[string]any{rpc.MethodSettingsGet: rpc.Settings{LeakGuardGroups: []string{"PROXY"}, HealthEgress: true}})
	s, ok := healthSettings(context.Background(), "/x.sock")
	if !ok || !slices.Equal(s.SensitiveGroups, []string{"PROXY"}) || !s.Egress {
		t.Fatalf("got %+v ok=%v", s, ok)
	}
	// Flags given override; absent ones keep the helper's.
	if err := doctorOverrides(&s, map[string]bool{"egress": true}, "", false); err != nil || s.Egress || !slices.Equal(s.SensitiveGroups, []string{"PROXY"}) {
		t.Fatalf("egress flag: %+v %v", s, err)
	}
	if err := doctorOverrides(&s, map[string]bool{"sensitive-groups": true}, "Google, Meta", false); err != nil || !slices.Equal(s.SensitiveGroups, []string{"Google", "Meta"}) {
		t.Fatalf("groups flag: %+v %v", s, err)
	}
	if err := doctorOverrides(&s, map[string]bool{"sensitive-groups": true}, " , ", false); err == nil {
		t.Fatal("empty --sensitive-groups accepted")
	}

	// No guard groups chosen yet, or no helper: the defaults.
	fakeHelperRPC(t, map[string]any{rpc.MethodSettingsGet: rpc.Settings{}})
	if s, ok = healthSettings(context.Background(), "/x.sock"); !ok || !slices.Equal(s.SensitiveGroups, health.DefaultSettings().SensitiveGroups) {
		t.Fatalf("no groups: %+v", s)
	}
	fakeHelperRPC(t, nil)
	if s, ok = healthSettings(context.Background(), "/x.sock"); ok || !slices.Equal(s.SensitiveGroups, health.DefaultSettings().SensitiveGroups) {
		t.Fatalf("no helper: %+v", s)
	}
}

func TestDoctorRejectsUnknownCheck(t *testing.T) {
	fakeHelperRPC(t, nil)
	for _, args := range [][]string{{"--only", "nosuch"}, {"--only", "leak-guard,nosuch"}, {"--fix", "nosuch"}} {
		err := runDoctor(args)
		if err == nil || !strings.Contains(err.Error(), `"nosuch"`) || !strings.Contains(err.Error(), "leak-guard") {
			t.Fatalf("%v: %v", args, err)
		}
		if strings.Contains(err.Error(), `"leak-guard"`) {
			t.Fatalf("%v: a known id reported as unknown: %v", args, err)
		}
	}
}
