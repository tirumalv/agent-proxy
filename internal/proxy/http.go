package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/agentproxy/agent-proxy/internal/detector"
	"github.com/agentproxy/agent-proxy/internal/logger"
)

// HTTPProxy is a transparent reverse proxy that logs all request/response pairs.
type HTTPProxy struct {
	target  *url.URL
	log     *logger.Logger
	reverse *httputil.ReverseProxy
}

func NewHTTP(targetURL string, l *logger.Logger) (*HTTPProxy, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid target URL: %w", err)
	}
	p := &HTTPProxy{target: target, log: l}
	p.reverse = httputil.NewSingleHostReverseProxy(target)
	// Suppress default error logging; we handle it.
	p.reverse.ErrorLog = log.New(io.Discard, "", 0)
	return p, nil
}

func (p *HTTPProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Buffer request body.
	reqBody, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(reqBody))

	proto := detector.Detect(r.URL.Path, r.Header.Get("Content-Type"), reqBody)

	// Log request.
	p.log.Add(logger.Entry{
		Timestamp: start,
		Protocol:  proto,
		Direction: logger.DirectionRequest,
		Method:    r.Method,
		Path:      r.URL.Path,
		Body:      logger.SafeBody(reqBody),
	})

	// Handle SSE streams specially.
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") ||
		strings.Contains(r.Header.Get("Content-Type"), "text/event-stream") {
		p.proxySSE(w, r, start, proto)
		return
	}

	// Capture response body via custom ResponseWriter.
	rec := &responseRecorder{ResponseWriter: w, buf: &bytes.Buffer{}}
	r.URL.Host = p.target.Host
	r.URL.Scheme = p.target.Scheme
	r.Host = p.target.Host

	// Reset body for upstream.
	r.Body = io.NopCloser(bytes.NewReader(reqBody))
	p.reverse.ServeHTTP(rec, r)

	p.log.Add(logger.Entry{
		Timestamp: time.Now(),
		Protocol:  proto,
		Direction: logger.DirectionResponse,
		Method:    r.Method,
		Path:      r.URL.Path,
		StatusCode: rec.status,
		Body:      logger.SafeBody(rec.buf.Bytes()),
		LatencyMs: time.Since(start).Milliseconds(),
	})
}

// proxySSE forwards Server-Sent Events chunk by chunk and logs each event.
func (p *HTTPProxy) proxySSE(w http.ResponseWriter, r *http.Request, start time.Time, proto detector.Protocol) {
	upReq, _ := http.NewRequestWithContext(r.Context(), r.Method, p.target.String()+r.RequestURI, r.Body)
	for k, vs := range r.Header {
		for _, v := range vs {
			upReq.Header.Add(k, v)
		}
	}
	upReq.Host = p.target.Host

	resp, err := http.DefaultClient.Do(upReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	flusher, canFlush := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(w, line)
		if canFlush {
			flusher.Flush()
		}
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			p.log.Add(logger.Entry{
				Timestamp: time.Now(),
				Protocol:  proto,
				Direction: logger.DirectionResponse,
				Method:    "SSE",
				Path:      r.URL.Path,
				StatusCode: resp.StatusCode,
				Body:      logger.SafeBody([]byte(strings.TrimSpace(data))),
				LatencyMs: time.Since(start).Milliseconds(),
			})
		}
	}
}

// responseRecorder buffers the response body while passing through to the real writer.
type responseRecorder struct {
	http.ResponseWriter
	buf    *bytes.Buffer
	status int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.buf.Write(b)
	return r.ResponseWriter.Write(b)
}
