package rpc

// MethodServiceLog returns the tail of what a managed service wrote to
// stdout and stderr (the files its service definition names, under the
// state dir): mihomo's startup errors, which its controller log stream
// never carries. Read-only.
const MethodServiceLog = "service.log"

// ServiceLogParams name the service ("mihomo") and how many lines to
// return per file (default 300, at most 2000).
type ServiceLogParams struct {
	Name  string `json:"name"`
	Lines int    `json:"lines,omitempty"`
}

// ServiceLog is the tail of each output file, oldest line first. URLs in
// the lines have their user info and queries redacted.
type ServiceLog struct {
	Name  string           `json:"name"`
	Files []ServiceLogFile `json:"files"`
}

// ServiceLogFile is one output file: its name (no directory), its size,
// and its last lines, the rotated copy's included when the file itself
// has fewer.
type ServiceLogFile struct {
	File  string   `json:"file"`
	Size  int64    `json:"size"`
	Lines []string `json:"lines"`
}
