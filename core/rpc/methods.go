package rpc

import "log/slog"

// MethodClass says what a method does to the helper's machine, which
// decides how a call is logged. One table (methods) holds every method's
// class and is the allowlist too, so a new method cannot be added without
// saying which it is.
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

// methods is every method the helper serves, with its class.
var methods = []struct {
	name  string
	class MethodClass
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
	{MethodConfigImport, ClassChange},

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
	{MethodTakeoverUndo, ClassChange},

	{MethodControllerSecure, ClassChange},
	{MethodControllerCredentials, ClassChange}, // hands out the secret
	{MethodControllerMove, ClassChange},

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
	{MethodTailscaleLogin, ClassChange},

	{MethodAutostartList, ClassRead},
	{MethodAutostartChange, ClassChange},

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

var classes = func() map[string]MethodClass {
	m := make(map[string]MethodClass, len(methods))
	for _, e := range methods {
		if _, dup := m[e.name]; dup {
			panic("rpc: method listed twice: " + e.name)
		}
		m[e.name] = e.class
	}
	return m
}()

// ClassOf is a method's class; 0 for a name not in the allowlist.
func ClassOf(method string) MethodClass { return classes[method] }
