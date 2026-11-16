package mcp

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"
)

func rpcLine(id int, method string, params string) string {
	if params == "" {
		return `{"jsonrpc":"2.0","id":` + itoa(id) + `,"method":"` + method + `"}`
	}
	return `{"jsonrpc":"2.0","id":` + itoa(id) + `,"method":"` + method + `","params":` + params + `}`
}

func itoa(i int) string { return strings.TrimSpace(jsonNumber(i)) }

func jsonNumber(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

type resp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func exchange(t *testing.T, s *Server, input string) []resp {
	t.Helper()
	var out strings.Builder
	if err := s.Serve(strings.NewReader(input), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var got []resp
	sc := bufio.NewScanner(strings.NewReader(out.String()))
	for sc.Scan() {
		var r resp
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("bad response line %q: %v", sc.Text(), err)
		}
		got = append(got, r)
	}
	return got
}

func testServer() *Server {
	return &Server{
		ServerInfo: map[string]any{"name": "test", "version": "0"},
		Tools:      []Tool{{Name: "echo", Description: "echo", InputSchema: map[string]any{"type": "object"}}},
		Call: func(name string, args json.RawMessage) (any, error) {
			if name == "fail" {
				return nil, errBoom
			}
			return TextResult("ok:" + name), nil
		},
	}
}

var errBoom = errString("boom")

type errString string

func (e errString) Error() string { return string(e) }

func TestInitializeAndList(t *testing.T) {
	in := rpcLine(1, "initialize", `{"protocolVersion":"2025-06-18"}`) + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		rpcLine(2, "tools/list", "") + "\n"
	rs := exchange(t, testServer(), in)
	if len(rs) != 2 {
		t.Fatalf("got %d responses, want 2 (notification must be silent)", len(rs))
	}
	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      map[string]any
	}
	if err := json.Unmarshal(rs[0].Result, &init); err != nil {
		t.Fatal(err)
	}
	if init.ProtocolVersion != "2025-06-18" {
		t.Errorf("protocolVersion = %q", init.ProtocolVersion)
	}
	if init.ServerInfo["name"] != "test" {
		t.Errorf("serverInfo wrong: %v", init.ServerInfo)
	}
	var list struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(rs[1].Result, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "echo" {
		t.Errorf("tools/list wrong: %+v", list.Tools)
	}
}

func TestCallAndErrors(t *testing.T) {
	s := testServer()
	in := rpcLine(1, "tools/call", `{"name":"echo","arguments":{}}`) + "\n" +
		rpcLine(2, "tools/call", `{"name":"fail","arguments":{}}`) + "\n" +
		rpcLine(3, "bogus/method", "") + "\n" +
		"not-json\n" +
		rpcLine(4, "ping", "") + "\n"
	rs := exchange(t, s, in)
	if len(rs) != 5 {
		t.Fatalf("got %d responses, want 5", len(rs))
	}
	if !strings.Contains(string(rs[0].Result), "ok:echo") || strings.Contains(string(rs[0].Result), "isError") {
		t.Errorf("call result wrong: %s", rs[0].Result)
	}
	if !strings.Contains(string(rs[1].Result), `"isError":true`) || !strings.Contains(string(rs[1].Result), "boom") {
		t.Errorf("tool error not surfaced as isError: %s", rs[1].Result)
	}
	if rs[2].Error == nil || rs[2].Error.Code != codeMethodNotFound {
		t.Errorf("unknown method: %+v", rs[2].Error)
	}
	if rs[3].Error == nil || rs[3].Error.Code != codeParseError {
		t.Errorf("bad json: %+v", rs[3].Error)
	}
	if rs[4].Error != nil {
		t.Errorf("ping should succeed: %+v", rs[4].Error)
	}
}

func TestEmptyLinesIgnored(t *testing.T) {
	rs := exchange(t, testServer(), "\n  \n"+rpcLine(1, "ping", "")+"\n\n")
	if len(rs) != 1 {
		t.Fatalf("got %d responses, want 1", len(rs))
	}
}
