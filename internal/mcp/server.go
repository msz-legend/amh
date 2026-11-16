// Package mcp implements a minimal Model Context Protocol server over
// stdio (JSON-RPC 2.0, newline-delimited), with no third-party dependencies.
// It supports the subset amh needs: initialize, ping, tools/list, tools/call.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
)

// Protocol versions we can speak; we echo the client's version when known.
const defaultProtocolVersion = "2024-11-05"

// JSON-RPC 2.0 wire types (subset).
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// JSON-RPC error codes used here.
const (
	codeParseError     = -32700
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// Handler processes a tools/call invocation and returns the result payload.
type Handler func(name string, args json.RawMessage) (any, error)

// Tool describes one exposed tool for tools/list.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Server bundles tool definitions and the call handler.
type Server struct {
	ServerInfo map[string]any
	Tools      []Tool
	Call       Handler
}

// Serve reads newline-delimited JSON-RPC requests from r and writes responses
// to w until EOF. Notifications (no id) get no response. It never returns an
// error for malformed single messages — those get a parse-error reply.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	out := bufio.NewWriter(w)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20) // allow large session payloads
	for sc.Scan() {
		line := sc.Bytes()
		if len(trimSpace(line)) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			writeResp(out, response{JSONRPC: "2.0", Error: &rpcError{
				Code: codeParseError, Message: "parse error: " + err.Error()}})
			continue
		}
		// Notification: no id field → no response expected.
		if len(req.ID) == 0 || string(req.ID) == "null" {
			continue
		}
		resp := s.dispatch(req)
		writeResp(out, resp)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return out.Flush()
}

func trimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && isSpace(b[start]) {
		start++
	}
	for end > start && isSpace(b[end-1]) {
		end--
	}
	return b[start:end]
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

func writeResp(w *bufio.Writer, resp response) {
	b, err := json.Marshal(resp)
	if err != nil {
		b, _ = json.Marshal(response{JSONRPC: "2.0", Error: &rpcError{
			Code: codeInternalError, Message: "result marshal: " + err.Error()}})
	}
	w.Write(b)
	w.WriteByte('\n')
	w.Flush()
}

func (s *Server) dispatch(req request) response {
	reply := func(result any, rpcErr *rpcError) response {
		return response{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr}
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		ver := p.ProtocolVersion
		if ver == "" {
			ver = defaultProtocolVersion
		}
		return reply(map[string]any{
			"protocolVersion": ver,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      s.ServerInfo,
		}, nil)
	case "ping":
		return reply(map[string]any{}, nil)
	case "tools/list":
		return reply(map[string]any{"tools": s.Tools}, nil)
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return reply(nil, &rpcError{Code: codeInvalidParams, Message: "bad params: " + err.Error()})
		}
		res, err := s.Call(p.Name, p.Arguments)
		if err != nil {
			// Tool-level failures are reported inside the result (isError),
			// per MCP convention, so the model can see and react.
			return reply(map[string]any{
				"content": []map[string]string{{"type": "text", "text": "error: " + err.Error()}},
				"isError": true,
			}, nil)
		}
		return reply(res, nil)
	default:
		return reply(nil, &rpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method})
	}
}

// TextResult wraps a plain string as an MCP tool result.
func TextResult(text string) map[string]any {
	return map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}},
	}
}

// Logf writes a line to stderr — stdout is reserved for the protocol.
func Logf(format string, args ...any) {
	log.SetFlags(0)
	log.SetOutput(stderrWriter{})
	log.Printf(format, args...)
}

type stderrWriter struct{}

func (stderrWriter) Write(p []byte) (int, error) {
	return fmt.Fprint(stdErr, p)
}

// stdErr is a var so tests can capture log output.
var stdErr = io.Discard
