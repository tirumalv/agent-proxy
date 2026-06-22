// Package approval implements a human-in-the-loop gate: consequential messages
// are parked until a human approves, edits, or rejects them via the web UI.
//
// The gate is opt-in. When disabled — or for messages the policy does not
// consider consequential — Review is a pass-through, so the proxy stays
// transparent by default.
package approval

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/agentproxy/agent-proxy/internal/detector"
)

// Verdict is a human decision delivered from the UI to a parked message.
type Verdict struct {
	Action string `json:"action"` // "approve" | "reject" | "edit"
	Body   string `json:"body"`   // replacement payload when Action == "edit"
}

// Pending is a message parked awaiting review. Its JSON form is what the UI
// renders on /api/pending.
type Pending struct {
	ID       uint64            `json:"id"`
	Protocol detector.Protocol `json:"protocol"`
	Method   string            `json:"method"`
	Path     string            `json:"path"`
	Body     json.RawMessage   `json:"body"`
	At       time.Time         `json:"at"`

	ch chan Verdict // buffered(1); receives the decision
}

// Result is what Review hands back to the proxy.
type Result struct {
	Forward bool   // false → block the message
	Body    []byte // payload to send upstream (may differ from the original on edit)
}

// Policy reports whether a message requires human approval.
type Policy func(proto detector.Protocol, method string) bool

// Gate parks messages that match its policy until a human resolves them.
type Gate struct {
	enabled bool
	timeout time.Duration
	policy  Policy

	mu      sync.Mutex
	pending map[uint64]*Pending
	counter uint64
}

// New constructs a gate. A nil policy gates every message when enabled.
func New(enabled bool, timeout time.Duration, policy Policy) *Gate {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return &Gate{
		enabled: enabled,
		timeout: timeout,
		policy:  policy,
		pending: make(map[uint64]*Pending),
	}
}

// Enabled reports whether the gate is active.
func (g *Gate) Enabled() bool { return g.enabled }

// Review blocks until the message is resolved by a human, the request is
// abandoned by the caller, or the timeout elapses (fail-closed). Messages that
// do not require approval return immediately with Forward=true.
func (g *Gate) Review(ctx context.Context, proto detector.Protocol, method, path string, body []byte) Result {
	if !g.enabled || (g.policy != nil && !g.policy(proto, method)) {
		return Result{Forward: true, Body: body}
	}

	g.mu.Lock()
	g.counter++
	id := g.counter
	p := &Pending{
		ID:       id,
		Protocol: proto,
		Method:   method,
		Path:     path,
		Body:     toRaw(body),
		At:       time.Now(),
		ch:       make(chan Verdict, 1),
	}
	g.pending[id] = p
	g.mu.Unlock()

	defer func() {
		g.mu.Lock()
		delete(g.pending, id)
		g.mu.Unlock()
	}()

	select {
	case v := <-p.ch:
		switch v.Action {
		case "reject":
			return Result{Forward: false}
		case "edit":
			if v.Body != "" {
				return Result{Forward: true, Body: []byte(v.Body)}
			}
			return Result{Forward: true, Body: body}
		default: // approve
			return Result{Forward: true, Body: body}
		}
	case <-ctx.Done(): // caller hung up — stop holding the slot
		return Result{Forward: false}
	case <-time.After(g.timeout):
		return Result{Forward: false}
	}
}

// resolve delivers a verdict to a parked message. Returns false if the id is
// unknown (expired or already decided).
func (g *Gate) resolve(id uint64, v Verdict) bool {
	g.mu.Lock()
	p, ok := g.pending[id]
	if ok {
		delete(g.pending, id) // claim it so a double-click can't double-send
	}
	g.mu.Unlock()
	if !ok {
		return false
	}
	p.ch <- v
	return true
}

// list returns a snapshot of currently-parked messages, oldest first.
func (g *Gate) list() []*Pending {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]*Pending, 0, len(g.pending))
	for _, p := range g.pending {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// PendingHandler serves GET /api/pending — the review queue.
func (g *Gate) PendingHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(g.list())
	}
}

// DecideHandler serves POST /api/decide with body {id, action, body?}.
func (g *Gate) DecideHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID     uint64 `json:"id"`
			Action string `json:"action"`
			Body   string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request body", http.StatusBadRequest)
			return
		}
		switch req.Action {
		case "approve", "reject", "edit":
		default:
			http.Error(w, "action must be approve, reject, or edit", http.StatusBadRequest)
			return
		}
		if g.resolve(req.ID, Verdict{Action: req.Action, Body: req.Body}) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "no such pending message (expired or already decided)", http.StatusNotFound)
	}
}

// DefaultPolicy gates the consequential actions: MCP tools/call and A2A/ACP
// submissions (POST). Discovery and status-poll traffic passes through.
func DefaultPolicy(proto detector.Protocol, method string) bool {
	switch proto {
	case detector.ProtocolMCP, detector.ProtocolMCPSSE:
		return method == "tools/call"
	case detector.ProtocolA2A, detector.ProtocolACP:
		return method == http.MethodPost
	default:
		return false
	}
}

func toRaw(b []byte) json.RawMessage {
	if json.Valid(b) {
		return json.RawMessage(b)
	}
	q, _ := json.Marshal(string(b))
	return q
}
