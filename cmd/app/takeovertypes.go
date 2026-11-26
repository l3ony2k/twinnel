package main

import (
	"time"

	"github.com/l3ony2k/duet/core/rpc"
)

// TODO-switch-to-rpc: the takeover.* and controller.secure names and types
// below mirror the "adopt" agent's work in progress (core/rpc on
// feat/adopt), which is not on main yet. After that merge, replace them
// with rpc.MethodTakeoverPlan / TakeoverPlanParams / TakeoverPlan /
// TakeoverPlansParams / TakeoverDiscardParams / AdoptParams / AdoptResult /
// ControllerSecureResult and delete this file. Against a helper without
// these methods the page says the helper is too old (-32601 / -32005).

const (
	methodTakeoverPlan     = "takeover.plan"
	methodTakeoverPlans    = "takeover.plans"
	methodTakeoverDiscard  = "takeover.discard"
	methodTakeoverAdopt    = "takeover.adopt"
	methodControllerSecure = "controller.secure"

	eventTakeoverChanged = "takeover.changed"
)

// Plan states.
const (
	planReady      = "ready"
	planAdopted    = "adopted"
	planRolledBack = "rolled_back"
	planFailed     = "failed"
)

type takeoverPlanParams struct {
	Base string `json:"base,omitempty"`
}

type takeoverPlansParams struct {
	PlanID string `json:"plan_id,omitempty"`
}

type takeoverDiscardParams struct {
	PlanID string `json:"plan_id"`
	Force  bool   `json:"force,omitempty"`
}

type takeoverLayer struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Kind  string   `json:"kind"`
	Stage string   `json:"stage"`
	Keys  []string `json:"keys,omitempty"`
	Rules int      `json:"rules,omitempty"`
}

type takeoverChange struct {
	Area       string `json:"area"`
	What       string `json:"what"`
	Deliberate bool   `json:"deliberate"`
	Why        string `json:"why,omitempty"`
}

// takeoverCandidate is a possible base subscription (core/takeover
// Candidate). Not in the adopt agent's summary yet: asked for in HANDOFF;
// the page shows them when present.
type takeoverCandidate struct {
	Path         string `json:"path"`
	Size         int64  `json:"size"`
	ProxiesSame  int    `json:"proxies_same"`
	ProxiesTotal int    `json:"proxies_total"`
	GroupsSame   int    `json:"groups_same"`
	GroupsTotal  int    `json:"groups_total"`
	Rules        int    `json:"rules"`
	CommonTail   int    `json:"common_tail"`
	HeadRules    int    `json:"head_rules"`
	Explains     bool   `json:"explains"`
	Chosen       bool   `json:"chosen,omitempty"`
	Error        string `json:"error,omitempty"`
}

type takeoverPlan struct {
	PlanID      string    `json:"plan_id"`
	CreatedAt   time.Time `json:"created_at"`
	State       string    `json:"state"`
	ConfirmHash string    `json:"confirm_hash"`

	LiveConfig      string `json:"live_config"`
	LiveSHA256      string `json:"live_sha256"`
	Base            string `json:"base,omitempty"`
	MihomoService   string `json:"mihomo_service,omitempty"`
	MihomoBinary    string `json:"mihomo_binary,omitempty"`
	MihomoVersion   string `json:"mihomo_version,omitempty"`
	Controller      string `json:"controller,omitempty"`
	SecretSet       bool   `json:"secret_set"`
	TailscaleSvc    string `json:"tailscale_service,omitempty"`
	SubscriptionURL bool   `json:"subscription_url"`

	Candidates  []takeoverCandidate `json:"candidates,omitempty"`
	Layers      []takeoverLayer     `json:"layers"`
	Changes     []takeoverChange    `json:"changes,omitempty"`
	Equivalent  bool                `json:"equivalent"`
	Validated   bool                `json:"validated"`
	ValidateErr []string            `json:"validate_errors,omitempty"`
	Warnings    []string            `json:"warnings,omitempty"`
	Summary     string              `json:"summary,omitempty"`
	SemDiff     string              `json:"semdiff,omitempty"`

	AdoptedAt time.Time `json:"adopted_at,omitzero"`
	Backup    string    `json:"backup,omitempty"`
}

type adoptParams struct {
	PlanID      string `json:"plan_id"`
	ConfirmHash string `json:"confirm_hash"`
	SkipHealth  bool   `json:"skip_health,omitempty"`
}

type adoptStep struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok, failed, skipped, warn
	Detail string `json:"detail,omitempty"`
}

type adoptCheck struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
}

type adoptResult struct {
	PlanID              string           `json:"plan_id"`
	OK                  bool             `json:"ok"`
	Steps               []adoptStep      `json:"steps"`
	Apply               *rpc.ApplyResult `json:"apply,omitempty"`
	HistoryID           string           `json:"history_id,omitempty"`
	BackupHistoryID     string           `json:"backup_history_id,omitempty"`
	Backup              string           `json:"backup,omitempty"`
	HealthBefore        []adoptCheck     `json:"health_before,omitempty"`
	HealthAfter         []adoptCheck     `json:"health_after,omitempty"`
	HealthAfterRollback []adoptCheck     `json:"health_after_rollback,omitempty"`
	Worse               []string         `json:"worse,omitempty"`
	RolledBack          bool             `json:"rolled_back"`
	Warnings            []string         `json:"warnings,omitempty"`
	Error               string           `json:"error,omitempty"`
}

type controllerSecureResult struct {
	Replaced bool             `json:"replaced"`
	Apply    *rpc.ApplyResult `json:"apply,omitempty"`
	Warnings []string         `json:"warnings,omitempty"`
}
