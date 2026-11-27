package rpc

import "time"

// MethodServiceLog returns the tail of a log file the helper keeps under
// the state dir: what mihomo's service wrote to stdout and stderr
// (startup errors, which its controller log stream never carries), or the
// helper's own log, which only administrators can open on Windows and
// Linux. Read-only.
const MethodServiceLog = "service.log"

// The logs service.log serves.
const (
	ServiceLogMihomo = "mihomo"
	ServiceLogHelper = "helper"
)

// ServiceLogSources are the names service.log accepts.
var ServiceLogSources = []string{ServiceLogMihomo, ServiceLogHelper}

// ServiceLogParams name the log (ServiceLogSources), how many lines to
// return per file (default 300, at most 2000) and, optionally, the
// earliest time wanted: lines before Since are dropped (a line without a
// time goes with the one before it). A helper before Since was added
// refuses the field; send it only when set.
type ServiceLogParams struct {
	Name  string    `json:"name"`
	Lines int       `json:"lines,omitempty"`
	Since time.Time `json:"since,omitzero"`
}

// ServiceLog is the tail of each file, oldest line first. URLs in the
// lines have their user info and queries redacted.
type ServiceLog struct {
	Name  string           `json:"name"`
	Files []ServiceLogFile `json:"files"`
}

// ServiceLogFile is one file: its name (no directory), its size, and its
// last lines, the rotated copy's included when the file itself has fewer.
type ServiceLogFile struct {
	File  string   `json:"file"`
	Size  int64    `json:"size"`
	Lines []string `json:"lines"`
}
