package logger

import (
	"container/ring"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/agentproxy/agent-proxy/internal/detector"
)

type Direction string

const (
	DirectionRequest  Direction = "request"
	DirectionResponse Direction = "response"
	DirectionStdioIn  Direction = "stdio-in"
	DirectionStdioOut Direction = "stdio-out"
)

type Entry struct {
	ID         uint64            `json:"id"`
	Timestamp  time.Time         `json:"timestamp"`
	Protocol   detector.Protocol `json:"protocol"`
	Direction  Direction         `json:"direction"`
	Method     string            `json:"method"`
	Path       string            `json:"path"`
	StatusCode int               `json:"status_code,omitempty"`
	Body       json.RawMessage   `json:"body"`
	LatencyMs  int64             `json:"latency_ms,omitempty"`
}

const capacity = 500

type Logger struct {
	mu      sync.RWMutex
	buf     *ring.Ring
	counter uint64
	// OnAdd is an optional hook called after each entry is stored.
	// Used to emit OTEL spans without creating an import cycle.
	OnAdd func(Entry)
}

func New() *Logger {
	return &Logger{buf: ring.New(capacity)}
}

func (l *Logger) Add(e Entry) {
	l.mu.Lock()
	l.counter++
	e.ID = l.counter
	l.buf.Value = e
	l.buf = l.buf.Next()
	hook := l.OnAdd
	l.mu.Unlock()

	if hook != nil {
		hook(e)
	}
}

func (l *Logger) Get(proto string, limit int) []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	all := make([]Entry, 0, capacity)
	l.buf.Do(func(v any) {
		if v == nil {
			return
		}
		e := v.(Entry)
		if proto == "" || string(e.Protocol) == proto {
			all = append(all, e)
		}
	})
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all
}

func (l *Logger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = ring.New(capacity)
}

// Handler returns an HTTP handler for the /api/messages endpoint.
func (l *Logger) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodDelete {
			l.Clear()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		proto := r.URL.Query().Get("protocol")
		limit := 100
		if s := r.URL.Query().Get("limit"); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				limit = n
			}
		}
		entries := l.Get(proto, limit)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	}
}

// SafeBody converts raw bytes to json.RawMessage, falling back to a quoted string.
func SafeBody(b []byte) json.RawMessage {
	if json.Valid(b) {
		return json.RawMessage(b)
	}
	quoted, _ := json.Marshal(string(b))
	return json.RawMessage(quoted)
}
