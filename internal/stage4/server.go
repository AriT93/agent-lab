// Package stage4 exposes the joke tools as an MCP (Model Context Protocol)
// server over stdio, so any MCP client (Claude Code, Claude Desktop, ...) can
// use them.
//
// Lesson: in stages 2–3 the tool list lived inside our agent, wired to one
// model API. MCP moves the tools behind a standard protocol: the *client*
// owns the model and the loop, the *server* only describes and runs tools.
// Our agent loop isn't needed here at all.
//
// The protocol is small enough to write by hand. It is JSON-RPC 2.0, one JSON
// message per line on stdin/stdout:
//
//	client → initialize            server → its name, version, capabilities
//	client → notifications/initialized   (no reply: notifications have no id)
//	client → tools/list            server → [{name, description, inputSchema}]
//	client → tools/call            server → {content: [{type:"text", text}], isError}
//
// stdout is the protocol channel, so all logging (the -trace output) goes to
// stderr. A tool failure is a normal result with isError=true, not a JSON-RPC
// error, so the calling model sees it and can retry, same as in stage 2/3.
package stage4

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/AriT93/agent-lab/internal/trace"
)

// ProtocolVersion is the MCP revision this server speaks. Clients that ask for
// another version are told this one and decide whether to continue.
const ProtocolVersion = "2025-06-18"

// Tool is what the server lists (Name, Description, InputSchema) plus what it
// does on tools/call (Run). It mirrors stage 3's Tool, with MCP's field names.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`

	Run func(ctx context.Context, args json.RawMessage) (string, error) `json:"-"`
}

type Server struct {
	Name, Version string
	Tools         []Tool
	Trace         *trace.Tracer
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // absent for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Standard JSON-RPC error codes.
const (
	codeParse          = -32700
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// Serve reads requests from r and writes responses to w until r closes.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	in := bufio.NewScanner(r)
	in.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	out := json.NewEncoder(w) // Encode appends the newline that frames each message
	for in.Scan() {
		line := in.Bytes()
		if len(line) == 0 {
			continue
		}
		s.Trace.Step("← client", line)
		resp := s.handle(ctx, line)
		if resp == nil {
			continue
		}
		s.Trace.Step("→ client", resp)
		if err := out.Encode(resp); err != nil {
			return err
		}
	}
	return in.Err()
}

// handle returns nil for notifications, which get no reply.
func (s *Server) handle(ctx context.Context, line []byte) *response {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return &response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{codeParse, err.Error()}}
	}
	if len(req.ID) == 0 {
		return nil
	}
	resp := &response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": s.Name, "version": s.Version},
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": s.Tools}
	case "tools/call":
		result, err := s.call(ctx, req.Params)
		if err != nil {
			resp.Error = &rpcError{codeInvalidParams, err.Error()}
		} else {
			resp.Result = result
		}
	default:
		resp.Error = &rpcError{codeMethodNotFound, "method not found: " + req.Method}
	}
	return resp
}

func (s *Server) call(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad tools/call params: %w", err)
	}
	for _, t := range s.Tools {
		if t.Name != p.Name {
			continue
		}
		if len(p.Arguments) == 0 {
			p.Arguments = json.RawMessage("{}")
		}
		text, err := t.Run(ctx, p.Arguments)
		if err != nil {
			text = err.Error()
		}
		return map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
			"isError": err != nil,
		}, nil
	}
	return nil, fmt.Errorf("unknown tool %q", p.Name)
}
