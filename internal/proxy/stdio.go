package proxy

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/agentproxy/agent-proxy/internal/detector"
	"github.com/agentproxy/agent-proxy/internal/logger"
)

// StdioProxy wraps a subprocess and intercepts its stdin/stdout JSON-RPC stream.
type StdioProxy struct {
	cmd  string
	args []string
	log  *logger.Logger
}

func NewStdio(cmdLine string, l *logger.Logger) *StdioProxy {
	parts := strings.Fields(cmdLine)
	return &StdioProxy{cmd: parts[0], args: parts[1:], log: l}
}

// Run starts the subprocess and blocks until it exits.
func (s *StdioProxy) Run() error {
	cmd := exec.Command(s.cmd, s.args...)

	upstreamIn, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	upstreamOut, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %q: %w", s.cmd, err)
	}

	// host → subprocess
	go s.pipe(os.Stdin, upstreamIn, logger.DirectionStdioIn)
	// subprocess → host
	go s.pipe(upstreamOut, os.Stdout, logger.DirectionStdioOut)

	if err := cmd.Wait(); err != nil {
		log.Printf("subprocess exited: %v", err)
	}
	return nil
}

func (s *StdioProxy) pipe(src io.Reader, dst io.Writer, dir logger.Direction) {
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		proto := detector.DetectStdio(line)
		s.log.Add(logger.Entry{
			Timestamp: time.Now(),
			Protocol:  proto,
			Direction: dir,
			Method:    extractMethod(line),
			Body:      logger.SafeBody(line),
		})
		dst.Write(line)
		dst.Write([]byte("\n"))
	}
}

func extractMethod(b []byte) string {
	// Quick heuristic: find "method":"<value>" without full JSON parse.
	s := string(b)
	const key = `"method":"`
	idx := strings.Index(s, key)
	if idx < 0 {
		return ""
	}
	rest := s[idx+len(key):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}
