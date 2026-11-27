package uninstall

import (
	"errors"
	"testing"

	"github.com/l3ony2k/duet/core/rpc"
)

func facts(info *rpc.HelperInfo) Facts {
	return Facts{
		Info: info, HelperService: "/L/com.l3ony2k.duet.helper.plist", HelperBinary: "/P/com.l3ony2k.duet.helper",
		Socket: "/var/run/duet/helper.sock", StateDir: "/S/duet", SecretStore: "keychain",
		OldPlistExists: func(string) bool { return true },
	}
}

func hardened() *rpc.HelperInfo {
	return &rpc.HelperInfo{
		StateDir:   "/S/duet",
		Mihomo:     rpc.HelperMihomoService{Label: "com.l3ony2k.duet.mihomo", Installed: true, Controller: "127.0.0.1:9090"},
		Hardened:   &rpc.HelperHardened{PlanID: "h1", OldLabel: "com.mihomo.proxy", OldPlist: "/L/com.mihomo.proxy.plist"},
		SecretKeys: []string{"controller-secret", "subscription-url/adopted"},
	}
}

func refusal(t *testing.T, err error, code string) {
	t.Helper()
	var r *Refusal
	if !errors.As(err, &r) || r.Code != code {
		t.Fatalf("err = %v, want refusal %s", err, code)
	}
}

func TestHardenedNeedsAChoice(t *testing.T) {
	_, err := Make(facts(hardened()), Options{})
	refusal(t, err, NeedsRestore)
	_, err = Make(facts(hardened()), Options{Purge: true})
	refusal(t, err, NeedsRestore)
	_, err = Make(facts(hardened()), Options{Purge: true, KeepMihomo: true})
	refusal(t, err, Conflict)
	_, err = Make(facts(hardened()), Options{RestoreMihomo: true, KeepMihomo: true})
	refusal(t, err, Conflict)
}

func TestHardenedRestore(t *testing.T) {
	p, err := Make(facts(hardened()), Options{RestoreMihomo: true, Purge: true})
	if err != nil {
		t.Fatal(err)
	}
	r := p.Restore
	if r == nil || r.FromLabel != "com.l3ony2k.duet.mihomo" || r.ToLabel != "com.mihomo.proxy" || r.ToPlist != "/L/com.mihomo.proxy.plist" || r.Controller != "127.0.0.1:9090" {
		t.Fatalf("restore = %+v", r)
	}
	for _, k := range []Kind{HelperService, HelperBinary, HelperSocket, DuetMihomo, StateDir, Secrets} {
		if !p.Removes(k) {
			t.Errorf("does not remove %s: %+v", k, p.Remove)
		}
	}
	if len(p.Keep) == 0 || p.Keep[0].Kind != OriginalMihomo {
		t.Errorf("keep = %+v", p.Keep)
	}
}

func TestHardenedRestoreNeedsOldPlist(t *testing.T) {
	f := facts(hardened())
	f.OldPlistExists = func(string) bool { return false }
	_, err := Make(f, Options{RestoreMihomo: true})
	refusal(t, err, NeedsRestore)
}

func TestHardenedKeep(t *testing.T) {
	p, err := Make(facts(hardened()), Options{KeepMihomo: true})
	if err != nil {
		t.Fatal(err)
	}
	if p.Restore != nil || p.Removes(StateDir) || p.Removes(DuetMihomo) {
		t.Fatalf("plan = %+v", p)
	}
}

func TestOwnMihomoRefusesPurge(t *testing.T) {
	info := &rpc.HelperInfo{Mihomo: rpc.HelperMihomoService{Label: "com.l3ony2k.duet.mihomo", Installed: true}}
	_, err := Make(facts(info), Options{Purge: true})
	refusal(t, err, OwnMihomo)
	p, err := Make(facts(info), Options{})
	if err != nil || p.Removes(StateDir) {
		t.Fatalf("plan = %+v, %v", p, err)
	}
}

func TestAdoptedPurge(t *testing.T) {
	info := &rpc.HelperInfo{StateDir: "/S/duet", Subscriptions: 1, Layers: 3,
		Mihomo: rpc.HelperMihomoService{Label: "com.mihomo.proxy", Adopted: true, Installed: true}}
	p, err := Make(facts(info), Options{Purge: true})
	if err != nil {
		t.Fatal(err)
	}
	if p.Restore != nil || !p.Removes(StateDir) || p.Removes(Secrets) { // no secret keys known
		t.Fatalf("plan = %+v", p)
	}
	if p.Keep[0].Kind != OriginalMihomo || p.Keep[0].Path != "com.mihomo.proxy" {
		t.Fatalf("keep = %+v", p.Keep)
	}
	if p.Remove[3].Subscriptions != 1 || p.Remove[3].Layers != 3 {
		t.Fatalf("state dir = %+v", p.Remove[3])
	}
}

func TestUnknown(t *testing.T) {
	p, err := Make(facts(nil), Options{Purge: true})
	if err != nil || p.Known || !p.Removes(StateDir) || !p.Removes(Secrets) {
		t.Fatalf("plan = %+v, %v", p, err)
	}
}
