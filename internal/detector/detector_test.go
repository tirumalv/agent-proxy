package detector

import (
	"testing"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		contentType string
		body        []byte
		want        Protocol
	}{
		// SSE takes highest priority regardless of path/body.
		{
			name:        "SSE content-type → mcp-sse",
			contentType: "text/event-stream",
			want:        ProtocolMCPSSE,
		},
		{
			name:        "SSE with path → still mcp-sse",
			path:        "/a2a",
			contentType: "text/event-stream; charset=utf-8",
			want:        ProtocolMCPSSE,
		},

		// Path-based detection.
		{
			name: "path /a2a → a2a",
			path: "/a2a",
			want: ProtocolA2A,
		},
		{
			name: "path /A2A uppercase → a2a",
			path: "/A2A/task",
			want: ProtocolA2A,
		},
		{
			name: "path /runs → acp",
			path: "/runs",
			want: ProtocolACP,
		},
		{
			name: "path /acp → acp",
			path: "/acp/v1",
			want: ProtocolACP,
		},

		// Body-based detection.
		{
			name: "jsonrpc 2.0 body → mcp",
			body: []byte(`{"jsonrpc":"2.0","method":"tools/list","id":1}`),
			want: ProtocolMCP,
		},
		{
			name: "wrong jsonrpc version → raw",
			body: []byte(`{"jsonrpc":"1.1","method":"foo"}`),
			want: ProtocolUnknown,
		},
		{
			name: "parts+role fields → a2a",
			body: []byte(`{"parts":[],"role":"user"}`),
			want: ProtocolA2A,
		},
		{
			name: "agent_id field → acp",
			body: []byte(`{"agent_id":"abc","input":"hello"}`),
			want: ProtocolACP,
		},
		{
			name: "unknown body → raw",
			body: []byte(`{"foo":"bar"}`),
			want: ProtocolUnknown,
		},

		// Edge cases.
		{
			name: "empty everything → raw",
			want: ProtocolUnknown,
		},
		{
			name: "invalid JSON body → raw",
			body: []byte(`not-json`),
			want: ProtocolUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Detect(tc.path, tc.contentType, tc.body)
			if got != tc.want {
				t.Errorf("Detect(%q, %q, %q) = %q, want %q",
					tc.path, tc.contentType, tc.body, got, tc.want)
			}
		})
	}
}

func TestDetectStdio(t *testing.T) {
	got := DetectStdio([]byte(`{"jsonrpc":"2.0","method":"initialize"}`))
	if got != ProtocolMCP {
		t.Errorf("DetectStdio MCP body: got %q, want %q", got, ProtocolMCP)
	}

	got = DetectStdio([]byte(`{"foo":"bar"}`))
	if got != ProtocolUnknown {
		t.Errorf("DetectStdio unknown body: got %q, want %q", got, ProtocolUnknown)
	}
}
