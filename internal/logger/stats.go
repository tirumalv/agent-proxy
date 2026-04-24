package logger

import (
	"encoding/json"
	"net/http"
	"sort"
)

// ProtocolStats holds latency percentiles and error counts for one protocol.
type ProtocolStats struct {
	Count      int     `json:"count"`
	ErrorCount int     `json:"error_count"`
	P50Ms      float64 `json:"p50_ms"`
	P95Ms      float64 `json:"p95_ms"`
	P99Ms      float64 `json:"p99_ms"`
}

// StatsResponse maps protocol name → ProtocolStats.
type StatsResponse map[string]ProtocolStats

// StatsHandler returns an HTTP handler for GET /api/stats.
// Stats are derived from response/stdio-out entries in the current ring buffer.
func (l *Logger) StatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		entries := l.Get("", 0) // all entries, no limit

		// Collect latencies and error counts per protocol from response-direction entries.
		type bucket struct {
			latencies []float64
			errors    int
		}
		buckets := map[string]*bucket{}

		for _, e := range entries {
			if e.Direction != DirectionResponse && e.Direction != DirectionStdioOut && e.Direction != DirectionReplayRes {
				continue
			}
			proto := string(e.Protocol)
			if buckets[proto] == nil {
				buckets[proto] = &bucket{}
			}
			b := buckets[proto]
			if e.LatencyMs > 0 {
				b.latencies = append(b.latencies, float64(e.LatencyMs))
			}
			if e.StatusCode >= 400 {
				b.errors++
			}
		}

		resp := make(StatsResponse)
		for proto, b := range buckets {
			sort.Float64s(b.latencies)
			resp[proto] = ProtocolStats{
				Count:      len(b.latencies),
				ErrorCount: b.errors,
				P50Ms:      percentile(b.latencies, 50),
				P95Ms:      percentile(b.latencies, 95),
				P99Ms:      percentile(b.latencies, 99),
			}
		}

		json.NewEncoder(w).Encode(resp)
	}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p / 100.0)
	return sorted[idx]
}
