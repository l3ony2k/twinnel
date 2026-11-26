package rpc

// TakeoverCandidate is a file next to the live config that may be the
// subscription it was built from (core/takeover Candidate without the
// modification time). Chosen marks the plan's base; takeover.plan with
// {base: path} re-plans with another one.
type TakeoverCandidate struct {
	Path         string `json:"path"`
	Size         int64  `json:"size"`
	ProxiesSame  int    `json:"proxies_same"`
	ProxiesTotal int    `json:"proxies_total"`
	GroupsSame   int    `json:"groups_same"`
	GroupsTotal  int    `json:"groups_total"`
	Rules        int    `json:"rules"`
	CommonTail   int    `json:"common_tail"`
	HeadRules    int    `json:"head_rules"`
	// SettingsDiffer counts top-level settings that differ from live.
	SettingsDiffer int    `json:"settings_differ"`
	Explains       bool   `json:"explains"` // all proxies, groups and rules carried over unchanged
	Chosen         bool   `json:"chosen,omitempty"`
	Error          string `json:"error,omitempty"`
}
