package logger

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentproxy/agent-proxy/internal/detector"
)

func TestStatsHandler(t *testing.T) {
	l := New()

	// Add some response entries with latency.
	l.Add(Entry{Protocol: detector.ProtocolMCP, Direction: DirectionResponse, LatencyMs: 10, StatusCode: 200})
	l.Add(Entry{Protocol: detector.ProtocolMCP, Direction: DirectionResponse, LatencyMs: 50, StatusCode: 200})
	l.Add(Entry{Protocol: detector.ProtocolMCP, Direction: DirectionResponse, LatencyMs: 200, StatusCode: 500})
	// Request-direction entries should be ignored in stats.
	l.Add(Entry{Protocol: detector.ProtocolMCP, Direction: DirectionRequest, LatencyMs: 999, StatusCode: 0})
	// A2A response.
	l.Add(Entry{Protocol: detector.ProtocolA2A, Direction: DirectionResponse, LatencyMs: 30, StatusCode: 200})

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rr := httptest.NewRecorder()
	l.StatsHandler()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp StatsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	mcp, ok := resp["mcp"]
	if !ok {
		t.Fatal("expected mcp key in stats response")
	}
	if mcp.Count != 3 {
		t.Errorf("expected mcp count=3, got %d", mcp.Count)
	}
	if mcp.ErrorCount != 1 {
		t.Errorf("expected mcp error_count=1, got %d", mcp.ErrorCount)
	}
	if mcp.P50Ms <= 0 {
		t.Errorf("expected positive P50, got %f", mcp.P50Ms)
	}

	a2a, ok := resp["a2a"]
	if !ok {
		t.Fatal("expected a2a key in stats response")
	}
	if a2a.Count != 1 {
		t.Errorf("expected a2a count=1, got %d", a2a.Count)
	}
}

func TestPercentile(t *testing.T) {
	tests := []struct {
		name   string
		data   []float64
		p      float64
		wantGt float64 // result should be > 0
	}{
		{"empty slice", []float64{}, 50, 0},
		{"single value p50", []float64{42}, 50, 0},
		{"p50 of sorted", []float64{10, 20, 30, 40, 50}, 50, 0},
		{"p99 of sorted", []float64{10, 20, 30, 40, 50}, 99, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := percentile(tc.data, tc.p)
			if len(tc.data) == 0 && got != 0 {
				t.Errorf("empty slice: expected 0, got %f", got)
			}
			if len(tc.data) > 0 && got <= 0 {
				t.Errorf("non-empty slice: expected > 0, got %f", got)
			}
		})
	}
}
