package svchost

// Revision is the service host's behaviour generation, printed by
// `twinnel-helper svchost --revision`. Bump it when a change to the host
// (supervision, its log file, how it answers the SCM) must reach services
// that run now: an upgrade then restarts the services running under an
// older host, and only then (scm.Host.Restarts). Any other change to the
// helper binary reaches the host's file at once and its services when
// they next restart.
//
// Hosts from before revisions report none (0).
//
//	1: mihomo's output kept and rotated by the host
const Revision = 1
