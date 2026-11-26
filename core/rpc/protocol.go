// Package rpc is the protocol between the duet helper (root daemon) and its
// clients (the app and the CLI): JSON-RPC 2.0, one JSON object per line, over
// a local stream socket (a unix socket on macOS/Linux; a named pipe on
// Windows later, see Transport).
//
// The helper only serves the fixed list of methods in api.go. No method takes
// a command line or a filesystem path.
package rpc

import (
	"encoding/json"
	"fmt"
)

// Version is the JSON-RPC version string.
const Version = "2.0"

// MaxMessageSize bounds one request or response line.
const MaxMessageSize = 16 << 20

// Request is a JSON-RPC 2.0 request. Notifications (no ID) are not used by
// duet; the server answers them with nothing.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error is a JSON-RPC 2.0 error object. It implements error, so handlers can
// return one to choose the code.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// Standard and duet error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternal       = -32603

	CodeUnauthorized = -32001 // peer is not allowed to talk to the helper
	CodeFailed       = -32002 // the operation ran and failed (validation, reload, download...)
	CodeNotFound     = -32003 // no such layer / subscription / history entry / version
	CodeBusy         = -32004 // another apply or install is running
)

// Errorf builds an *Error.
func Errorf(code int, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}
