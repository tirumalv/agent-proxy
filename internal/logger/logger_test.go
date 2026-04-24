package logger

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agentproxy/agent-proxy/internal/detector"
)

func makeEntry(id uint64, proto detector.Protocol, dir Direction, latency int64, status int) Entry {
	return Entry{
		ID:         id,
		Timestamp:  time.Now(),
		Protocol:   proto,
		Direction:  dir,
		Method:     "test/method",
		LatencyMs:  latency,
		StatusCode: status,
	}
}

func TestLoggerAddAndGet(t *testing.T) {
	l := New()

	e1 := makeEntry(0, detector.ProtocolMCP, DirectionRequest, 10, 0)
	e2 := makeEntry(0, detector.ProtocolA2A, DirectionResponse, 20, 200)
	l.Add(e1)
	l.Add(e2)

	all := l.Get("", 0)
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}
	// IDs should be auto-assigned starting at 1.
	if all[0].ID != 1 || all[1].ID != 2 {
		t.Errorf("unexpected IDs: %d, %d", all[0].ID, all[1].ID)
	}
}

func TestLoggerFilterByProtocol(t *testing.T) {
	l := New()
	l.Add(makeEntry(0, detector.ProtocolMCP, DirectionRequest, 5, 0))
	l.Add(makeEntry(0, detector.ProtocolA2A, DirectionResponse, 5, 200))
	l.Add(makeEntry(0, detector.ProtocolMCP, DirectionResponse, 5, 200))

	mcp := l.Get("mcp", 0)
	if len(mcp) != 2 {
		t.Errorf("expected 2 MCP entries, got %d", len(mcp))
	}
	a2a := l.Get("a2a", 0)
	if len(a2a) != 1 {
		t.Errorf("expected 1 A2A entry, got %d", len(a2a))
	}
}

func TestLoggerLimit(t *testing.T) {
	l := New()
	for i := 0; i < 10; i++ {
		l.Add(makeEntry(0, detector.ProtocolMCP, DirectionRequest, 1, 0))
	}
	limited := l.Get("", 3)
	if len(limited) != 3 {
		t.Errorf("expected 3 entries with limit, got %d", len(limited))
	}
}

func TestLoggerGetByID(t *testing.T) {
	l := New()
	l.Add(makeEntry(0, detector.ProtocolMCP, DirectionRequest, 5, 0))
	l.Add(makeEntry(0, detector.ProtocolA2A, DirectionResponse, 10, 200))

	e, ok := l.GetByID(1)
	if !ok {
		t.Fatal("expected to find entry with ID 1")
	}
	if e.Protocol != detector.ProtocolMCP {
		t.Errorf("unexpected protocol: %s", e.Protocol)
	}

	_, ok = l.GetByID(999)
	if ok {
		t.Error("expected not to find entry with ID 999")
	}
}

func TestLoggerClear(t *testing.T) {
	l := New()
	l.Add(makeEntry(0, detector.ProtocolMCP, DirectionRequest, 5, 0))
	l.Clear()
	all := l.Get("", 0)
	if len(all) != 0 {
		t.Errorf("expected 0 entries after clear, got %d", len(all))
	}
}

func TestLoggerHooks(t *testing.T) {
	l := New()
	var called []uint64
	l.AddHook(func(e Entry) { called = append(called, e.ID) })
	l.AddHook(func(e Entry) { called = append(called, e.ID*10) })

	l.Add(makeEntry(0, detector.ProtocolMCP, DirectionRequest, 5, 0))

	if len(called) != 2 {
		t.Fatalf("expected 2 hook calls, got %d", len(called))
	}
	if called[0] != 1 || called[1] != 10 {
		t.Errorf("unexpected hook values: %v", called)
	}
}

func TestLoggerHandler(t *testing.T) {
	l := New()
	l.Add(makeEntry(0, detector.ProtocolMCP, DirectionRequest, 5, 0))
	l.Add(makeEntry(0, detector.ProtocolA2A, DirectionResponse, 10, 200))

	// Test GET all.
	req := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	rr := httptest.NewRecorder()
	l.Handler()(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	var entries []Entry
	if err := json.NewDecoder(rr.Body).Decode(&entries); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}

	// Test DELETE clears the log.
	req = httptest.NewRequest(http.MethodDelete, "/api/messages", nil)
	rr = httptest.NewRecorder()
	l.Handler()(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rr.Code)
	}
	if got := l.Get("", 0); len(got) != 0 {
		t.Errorf("expected empty after DELETE, got %d entries", len(got))
	}
}

func TestSafeBody(t *testing.T) {
	// Valid JSON passes through.
	raw := []byte(`{"key":"value"}`)
	result := SafeBody(raw)
	if string(result) != string(raw) {
		t.Errorf("expected %s, got %s", raw, result)
	}

	// Invalid JSON is quoted as a string.
	invalid := []byte(`not json`)
	result = SafeBody(invalid)
	var s string
	if err := json.Unmarshal(result, &s); err != nil {
		t.Errorf("expected quoted string, got unmarshal error: %v", err)
	}
	if s != "not json" {
		t.Errorf("expected 'not json', got %q", s)
	}
}
