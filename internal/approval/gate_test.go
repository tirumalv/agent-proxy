package approval

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentproxy/agent-proxy/internal/detector"
)

func TestDefaultPolicy(t *testing.T) {
	cases := []struct {
		proto  detector.Protocol
		method string
		want   bool
	}{
		{detector.ProtocolMCP, "tools/call", true},
		{detector.ProtocolMCP, "initialize", false},
		{detector.ProtocolMCP, "tools/list", false},
		{detector.ProtocolMCPSSE, "tools/call", true},
		{detector.ProtocolA2A, http.MethodPost, true},
		{detector.ProtocolA2A, http.MethodGet, false},
		{detector.ProtocolACP, http.MethodPost, true},
		{detector.ProtocolUnknown, http.MethodPost, false},
	}
	for _, c := range cases {
		if got := DefaultPolicy(c.proto, c.method); got != c.want {
			t.Errorf("DefaultPolicy(%s, %s) = %v, want %v", c.proto, c.method, got, c.want)
		}
	}
}

func TestReviewPassThrough(t *testing.T) {
	body := []byte(`{"x":1}`)

	// Disabled gate forwards everything.
	g := New(false, time.Minute, DefaultPolicy)
	if r := g.Review(context.Background(), detector.ProtocolMCP, "tools/call", "/mcp", body); !r.Forward {
		t.Fatal("disabled gate must forward")
	}

	// Enabled, but a non-consequential method is not parked.
	g = New(true, time.Minute, DefaultPolicy)
	if r := g.Review(context.Background(), detector.ProtocolMCP, "initialize", "/mcp", body); !r.Forward {
		t.Fatal("non-consequential method must forward")
	}
	if n := len(g.list()); n != 0 {
		t.Fatalf("expected nothing parked, got %d", n)
	}
}

// reviewAsync runs a gated Review in the background, returning a channel for its result.
func reviewAsync(g *Gate, body []byte) <-chan Result {
	ch := make(chan Result, 1)
	go func() {
		ch <- g.Review(context.Background(), detector.ProtocolMCP, "tools/call", "/mcp", body)
	}()
	return ch
}

// waitParked polls until exactly one message is parked, returning its id.
func waitParked(t *testing.T, g *Gate) uint64 {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p := g.list(); len(p) == 1 {
			return p[0].ID
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("message never parked")
	return 0
}

func TestReviewApprove(t *testing.T) {
	g := New(true, time.Minute, DefaultPolicy)
	body := []byte(`{"a":1}`)
	res := reviewAsync(g, body)
	id := waitParked(t, g)
	if !g.resolve(id, Verdict{Action: "approve"}) {
		t.Fatal("resolve returned false")
	}
	got := <-res
	if !got.Forward || string(got.Body) != string(body) {
		t.Fatalf("approve: forward=%v body=%s", got.Forward, got.Body)
	}
	if n := len(g.list()); n != 0 {
		t.Fatalf("pending not cleaned up: %d", n)
	}
}

func TestReviewReject(t *testing.T) {
	g := New(true, time.Minute, DefaultPolicy)
	res := reviewAsync(g, []byte(`{}`))
	id := waitParked(t, g)
	g.resolve(id, Verdict{Action: "reject"})
	if got := <-res; got.Forward {
		t.Fatal("reject must not forward")
	}
}

func TestReviewEdit(t *testing.T) {
	g := New(true, time.Minute, DefaultPolicy)
	res := reviewAsync(g, []byte(`{"old":true}`))
	id := waitParked(t, g)
	edited := `{"new":true}`
	g.resolve(id, Verdict{Action: "edit", Body: edited})
	got := <-res
	if !got.Forward || string(got.Body) != edited {
		t.Fatalf("edit: forward=%v body=%s", got.Forward, got.Body)
	}
}

func TestReviewTimeoutFailsClosed(t *testing.T) {
	g := New(true, 50*time.Millisecond, DefaultPolicy)
	got := g.Review(context.Background(), detector.ProtocolMCP, "tools/call", "/mcp", []byte(`{}`))
	if got.Forward {
		t.Fatal("timeout must fail closed")
	}
	if n := len(g.list()); n != 0 {
		t.Fatalf("pending not cleaned up after timeout: %d", n)
	}
}

func TestReviewContextCancel(t *testing.T) {
	g := New(true, time.Minute, DefaultPolicy)
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan Result, 1)
	go func() {
		ch <- g.Review(ctx, detector.ProtocolMCP, "tools/call", "/mcp", []byte(`{}`))
	}()
	waitParked(t, g)
	cancel()
	if got := <-ch; got.Forward {
		t.Fatal("cancelled request must not forward")
	}
}

func TestDecideHandlerValidation(t *testing.T) {
	g := New(true, time.Minute, DefaultPolicy)

	req := httptest.NewRequest(http.MethodPost, "/api/decide", strings.NewReader(`{"id":1,"action":"nope"}`))
	w := httptest.NewRecorder()
	g.DecideHandler()(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad action: got %d, want 400", w.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/decide", strings.NewReader(`{"id":999,"action":"approve"}`))
	w = httptest.NewRecorder()
	g.DecideHandler()(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown id: got %d, want 404", w.Code)
	}
}

func TestPendingHandlerJSON(t *testing.T) {
	g := New(true, time.Minute, DefaultPolicy)
	done := reviewAsync(g, []byte(`{"k":"v"}`))
	id := waitParked(t, g)

	w := httptest.NewRecorder()
	g.PendingHandler()(w, httptest.NewRequest(http.MethodGet, "/api/pending", nil))

	var got []Pending
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 1 || got[0].Method != "tools/call" {
		t.Fatalf("unexpected pending payload: %+v", got)
	}

	g.resolve(id, Verdict{Action: "reject"})
	<-done
}
