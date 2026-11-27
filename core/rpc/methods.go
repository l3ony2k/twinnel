package rpc

import (
	"encoding/json"
	"log/slog"
)

// MethodClass says what a call does to the helper's machine, which
// decides how it is logged. One table (methods) holds every method's
// class and is the allowlist too, so a new method cannot be added without
// saying which it is. A method with a read mode and a write mode (a dry
// run, a preview, tailscale.login without start) says in the same table
// which params make a call a read (readWhen); ClassOfCall applies it.
type MethodClass uint8

const (
	// ClassRead changes nothing on the machine, in the settings or in the
	// stored configuration (it may refresh a cache: kernel.check keeps its
	// answer). Clients poll these, so a call is logged at DEBUG.
	ClassRead MethodClass = iota + 1
	// ClassChange changes something, hands out a secret
	// (controller.credentials, config.export), or runs probes someone
	// asked for (health.run, health.egress: their report replaces the
	// last). A call is logged at INFO: helper.log is the record of who
	// did what.
	ClassChange
	// ClassWait is a long poll (events.wait): one per client at all
	// times, never logged.
	ClassWait
)

func (c MethodClass) String() string {
	switch c {
	case ClassRead:
		return "read"
	case ClassChange:
		return "change"
	case ClassWait:
		return "wait"
	}
	return "unknown"
}

// LogLevel is the level a call of this class is logged at; ok is false
// when it is not logged.
func (c MethodClass) LogLevel() (level slog.Level, ok bool) {
	switch c {
	case ClassRead:
		return slog.LevelDebug, true
	case ClassWait:
		return 0, false
	}
	return slog.LevelInfo, true // ClassChange, and anything unknown
}

// classRule is a method's entry in the table: a MethodClass, or a
// readWhen rule for a method whose class depends on its params.
type classRule interface {
	// method is the class of the method as a whole: its worst call.
	method() MethodClass
	// call is the class of one call with these params.
	call(params json.RawMessage) MethodClass
}

func (c MethodClass) method() MethodClass              { return c }
func (c MethodClass) call(json.RawMessage) MethodClass { return c }

// readWhen is the rule of a change method with a read mode: a call whose
// params (decoded as P) satisfy read changes nothing and is a read; any
// other call, and params that do not decode, are a change.
func readWhen[P any](read func(P) bool) classRule { return paramRule[P](read) }

type paramRule[P any] func(P) bool

func (paramRule[P]) method() MethodClass { return ClassChange }

func (r paramRule[P]) call(params json.RawMessage) MethodClass {
	var p P
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return ClassChange
		}
	}
	if r(p) {
		return ClassRead
	}
	return ClassChange
}

// methods is every method the helper serves, with its class (or, for a
// method with a read mode, its rule).
//
// Audit (2026-11-28) of the methods with a read mode: tailscale.login
// without start (setup status), autostart.change with preview,
// config.import with dry_run, takeover.undo with dry_run, controller.move
// with dry_run. A new param that turns a call into a pure read belongs
// here too.
var methods = []struct {
	name  string
	class classRule
}{
	{MethodStatus, ClassRead},
	{MethodHelperInfo, ClassRead},
	{MethodHelperInventory, ClassRead},
	{MethodHelperRestart, ClassChange},

	{MethodComposeStatus, ClassRead},
	{MethodComposePreview, ClassRead},
	{MethodComposeApply, ClassChange},
	{MethodHistoryList, ClassRead},
	{MethodHistoryRollback, ClassChange},

	{MethodLayerList, ClassRead},
	{MethodLayerGet, ClassRead},
	{MethodLayerPut, ClassChange},
	{MethodLayerDelete, ClassChange},
	{MethodLayerSetEnabled, ClassChange},
	{MethodLayerReorder, ClassChange},
	{MethodLayerRestore, ClassChange},

	{MethodSubList, ClassRead},
	{MethodSubPut, ClassChange},
	{MethodSubDelete, ClassChange},
	{MethodSubActivate, ClassChange},
	{MethodSubRefresh, ClassChange},

	{MethodSettingsGet, ClassRead},
	{MethodSettingsUpdate, ClassChange},
	{MethodTailnetRefresh, ClassChange}, // re-applies when the peers changed

	{MethodKernelList, ClassRead},
	{MethodKernelCheck, ClassRead},
	{MethodKernelInstall, ClassChange},
	{MethodKernelRollback, ClassChange},
	{MethodTailscaleInfo, ClassRead},

	{MethodServiceStatus, ClassRead},
	{MethodServiceLog, ClassRead},
	{MethodServiceStart, ClassChange},
	{MethodServiceStop, ClassChange},
	{MethodServiceRestart, ClassChange},
	{MethodServiceInstall, ClassChange},
	{MethodServiceUninstall, ClassChange},

	{MethodHealthLast, ClassRead},
	{MethodHealthRun, ClassChange},    // on request; the app polls health.last
	{MethodHealthEgress, ClassChange}, // sends a request out through the proxy

	{MethodConfigGet, ClassRead}, // the secret is removed (ConfigText)
	{MethodConfigExport, ClassChange},
	{MethodConfigImport, readWhen(func(p ConfigImportParams) bool { return p.DryRun })},

	{MethodGeoStatus, ClassRead},
	{MethodGeoCheck, ClassRead},
	{MethodGeoUpdate, ClassChange},
	{MethodUIStatus, ClassRead},
	{MethodUICheck, ClassRead},
	{MethodUIUpdate, ClassChange},

	{MethodTakeoverPlan, ClassChange}, // writes a plan
	{MethodTakeoverPlans, ClassRead},
	{MethodTakeoverDiscard, ClassChange},
	{MethodTakeoverAdopt, ClassChange},
	{MethodTakeoverUndo, readWhen(func(p TakeoverUndoParams) bool { return p.DryRun })},

	{MethodControllerSecure, ClassChange},
	{MethodControllerCredentials, ClassChange},                                              // hands out the secret
	{MethodControllerMove, readWhen(func(p ControllerMoveParams) bool { return p.DryRun })}, // dry_run only picks the port

	{MethodAgentRecord, ClassChange},
	{MethodAgentActivity, ClassRead},

	{MethodHardenPlan, ClassChange}, // writes a plan
	{MethodHardenPlans, ClassRead},
	{MethodHardenApply, ClassChange},

	{MethodDNSStatus, ClassRead},
	{MethodDNSPoint, ClassChange},
	{MethodDNSRestore, ClassChange},
	{MethodDNSBackup, ClassChange},
	{MethodTailscaleInstall, ClassChange},
	{MethodTailscaleLogin, readWhen(func(p TailscaleLoginParams) bool { return !p.Start })}, // without start: the login state

	{MethodAutostartList, ClassRead},
	{MethodAutostartChange, readWhen(func(p AutostartChangeParams) bool { return p.Preview })},

	{MethodEventsWait, ClassWait},
}

// AllMethods is the allowlist, in the table's order. Server.Handle panics
// for any other name.
var AllMethods = func() []string {
	out := make([]string, len(methods))
	for i, m := range methods {
		out[i] = m.name
	}
	return out
}()

var classes = func() map[string]classRule {
	m := make(map[string]classRule, len(methods))
	for _, e := range methods {
		if _, dup := m[e.name]; dup {
			panic("rpc: method listed twice: " + e.name)
		}
		m[e.name] = e.class
	}
	return m
}()

// ClassOf is a method's class, the class of its worst call; 0 for a name
// not in the allowlist.
func ClassOf(method string) MethodClass {
	if r, ok := classes[method]; ok {
		return r.method()
	}
	return 0
}

// ClassOfCall is the class of one call: the method's, or for a method with
// a read mode what its params ask for. 0 for a name not in the allowlist.
func ClassOfCall(method string, params json.RawMessage) MethodClass {
	if r, ok := classes[method]; ok {
		return r.call(params)
	}
	return 0
}
