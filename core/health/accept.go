package health

// Accepted problems: a trade-off the user chose (a Tailscale exit node
// while mihomo's TUN is on) is a state, not a failure. Checks name what a
// problem comes from (Result.Causes); the app passes the causes the user
// accepted to Accept, which marks those results StatusAccepted, groups
// the checks the same cause breaks under them, and leaves both out of
// the counts. Checks still run and report the raw result.

// StatusAccepted is a problem the user accepted. It counts as neither a
// warning nor a failure.
const StatusAccepted Status = "accepted"

// Cause codes (Result.Causes).
const (
	// CauseExitNode: Tailscale routes the default route through an exit
	// node.
	CauseExitNode = "exit-node"
	CauseOtherVPN = "other-vpn"
	CauseTSNE     = "tailscale-ne"    // Tailscale's network extension holds a VPN slot
	CauseOtherTUN = "other-tun"       // another tunnel holds the default route
	CauseRoute    = "tun-not-default" // mihomo's TUN does not hold the default route
)

// Fix action codes (FixHint.AutoFix): the app maps each to a button.
const (
	FixCoexist     = "tailscale-coexist-override"
	FixLeakGuard   = "leak-guard-override"
	FixSystemDNS   = "system-dns"
	FixController  = "controller-secret"
	FixExitNodeOff = "exit-node-off"
	FixEgressProbe = "egress-probe" // run the opt-in egress test once
)

// causeEffects are the checks a cause can break besides the one that
// names it: an exit node takes the default route, so tailnet traffic and
// MagicDNS can go the wrong way too.
var causeEffects = map[string][]string{
	CauseExitNode: {"tailnet-route", "magicdns", "peer-short-names", "tailnet-resolver-binding", "leak-guard-egress"},
}

// Accept returns r with the results whose causes are all in accepted
// marked StatusAccepted (Was keeps the raw status), and the failing or
// warning checks such a cause breaks marked StatusAccepted with CausedBy
// set to the accepting check's ID. Overall is recomputed. r itself is not
// changed; with nothing accepted it is returned as is.
func Accept(r *Report, accepted ...string) *Report {
	if r == nil || len(accepted) == 0 {
		return r
	}
	ok := map[string]bool{}
	for _, a := range accepted {
		ok[a] = true
	}
	out := *r
	out.Checks = append([]CheckReport(nil), r.Checks...)
	changed := false
	causedBy := map[string]string{} // check ID → accepting check's ID
	for i := range out.Checks {
		cr := &out.Checks[i]
		if !problem(cr.Status) || len(cr.Causes) == 0 {
			continue
		}
		all := true
		for _, c := range cr.Causes {
			all = all && ok[c]
		}
		if !all {
			continue
		}
		cr.Was, cr.Status, changed = cr.Status, StatusAccepted, true
		for _, c := range cr.Causes {
			for _, id := range causeEffects[c] {
				if _, set := causedBy[id]; !set {
					causedBy[id] = cr.ID
				}
			}
		}
	}
	for i := range out.Checks {
		cr := &out.Checks[i]
		if by, hit := causedBy[cr.ID]; hit && problem(cr.Status) {
			cr.Was, cr.Status, cr.CausedBy, changed = cr.Status, StatusAccepted, by, true
		}
	}
	if !changed {
		return r
	}
	out.Overall = StatusOK
	for _, c := range out.Checks {
		out.Overall = Worse(out.Overall, c.Status)
	}
	return &out
}

func problem(s Status) bool { return s == StatusWarn || s == StatusFail }

// EvidenceLine is one distinct evidence line and how often it occurred.
type EvidenceLine struct {
	Text  string
	Count int
}

// DedupeEvidence folds repeated evidence lines into one each, in the
// order they first appear, with how often they occurred.
func DedupeEvidence(lines []string) []EvidenceLine {
	var out []EvidenceLine
	at := map[string]int{}
	for _, l := range lines {
		if i, seen := at[l]; seen {
			out[i].Count++
			continue
		}
		at[l] = len(out)
		out = append(out, EvidenceLine{Text: l, Count: 1})
	}
	return out
}
