package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// NewFileHook opens (or creates) a file at path for append-only NDJSON writing.
// Returns a hook function to pass to Logger.AddHook, a close function, and any error.
func NewFileHook(path string) (hook func(Entry), close func() error, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file %q: %w", path, err)
	}

	var mu sync.Mutex
	hook = func(e Entry) {
		b, err := json.Marshal(e)
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		f.Write(b)
		f.Write([]byte("\n"))
	}
	close = f.Close
	return hook, close, nil
}
