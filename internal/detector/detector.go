package detector

import (
	"encoding/json"
	"strings"
)

type Protocol string

const (
	ProtocolMCP     Protocol = "mcp"
	ProtocolMCPSSE  Protocol = "mcp-sse"
	ProtocolA2A     Protocol = "a2a"
	ProtocolACP     Protocol = "acp"
	ProtocolUnknown Protocol = "raw"
)

// Detect identifies the protocol from HTTP request metadata and body bytes.
func Detect(path, contentType string, body []byte) Protocol {
	if strings.Contains(contentType, "text/event-stream") {
		return ProtocolMCPSSE
	}

	lowerPath := strings.ToLower(path)

	if strings.Contains(lowerPath, "/a2a") {
		return ProtocolA2A
	}
	if strings.Contains(lowerPath, "/runs") || strings.Contains(lowerPath, "/acp") {
		return ProtocolACP
	}

	if len(body) > 0 {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(body, &m); err == nil {
			if v, ok := m["jsonrpc"]; ok {
				var s string
				if json.Unmarshal(v, &s) == nil && s == "2.0" {
					return ProtocolMCP
				}
			}
			_, hasParts := m["parts"]
			_, hasRole := m["role"]
			if hasParts && hasRole {
				return ProtocolA2A
			}
			_, hasAgentID := m["agent_id"]
			if hasAgentID {
				return ProtocolACP
			}
		}
	}

	return ProtocolUnknown
}

// DetectStdio identifies protocol from raw newline-delimited JSON bytes (stdio transport).
func DetectStdio(body []byte) Protocol {
	return Detect("", "", body)
}
