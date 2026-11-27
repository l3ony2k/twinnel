package agent

// The stdio transport for `twinnel mcp`. MCP's stdio framing is one JSON-RPC
// message per line. Clients differ in what else they put on the wire: a
// PowerShell StreamWriter with the default UTF-8 encoding writes a byte order
// mark before the first message, some clients send blank lines or CRLF. The
// SDK's transport stops on the first byte that is not JSON, and the client
// only sees the pipe close. This one cleans every line first and answers a
// line that is not JSON with a JSON-RPC parse error (and a note on stderr),
// then keeps serving.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewStdioTransport serves MCP on in/out (os.Stdin, os.Stdout); problems with
// the input are reported on errw (os.Stderr).
func NewStdioTransport(in io.Reader, out, errw io.Writer) mcp.Transport {
	w := &lockedWriter{w: out}
	pr, pw := io.Pipe()
	go filterStdio(in, pw, w, errw)
	return &mcp.IOTransport{Reader: pr, Writer: w}
}

// lockedWriter serialises writes: the SDK writes one message per Write, and
// filterStdio writes parse errors between them.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (l *lockedWriter) Close() error { return nil }

// stdioJunk is what may surround a message on its line: whitespace and byte
// order marks (U+FEFF).
const stdioJunk = " \t\r\n\ufeff"

// filterStdio copies one cleaned message per line from in to pw. A line that
// is not JSON is answered on out with a parse error and logged on errw.
func filterStdio(in io.Reader, pw *io.PipeWriter, out, errw io.Writer) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), mcp.DefaultMaxLineLength)
	for sc.Scan() {
		line := bytes.Trim(sc.Bytes(), stdioJunk)
		if len(line) == 0 {
			continue
		}
		var v json.RawMessage
		if err := json.Unmarshal(line, &v); err != nil {
			fmt.Fprintf(errw, "twinnel mcp: ignored a line that is not JSON (%v): %q\n", err, preview(line))
			b, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": nil,
				"error": map[string]any{"code": -32700, "message": "Parse error: each line must be one JSON-RPC message (" + err.Error() + ")"},
			})
			out.Write(append(b, '\n'))
			continue
		}
		msg := make([]byte, 0, len(line)+1) // line points into the scanner's buffer
		msg = append(append(msg, line...), '\n')
		if _, err := pw.Write(msg); err != nil {
			return // the server stopped
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(errw, "twinnel mcp: reading stdin: %v\n", err)
		pw.CloseWithError(err)
		return
	}
	pw.Close()
}

func preview(b []byte) string {
	const n = 40
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}
