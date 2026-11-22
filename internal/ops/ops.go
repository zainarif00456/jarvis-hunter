// Package ops implements what Jarvis can ask this machine to do. Nothing here limits what is
// asked: the person at the machine is protected by the visible indicator, the Stop button and the
// action log, not by refusing work.
package ops

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Sender is how an operation talks back while it runs.
type Sender interface {
	// Event streams something about a running request (output, status) to the server.
	Event(id string, fields map[string]any)
	// Bytes sends a bulk frame (terminal output, video, file chunk).
	Bytes(ch byte, id string, payload []byte) error
}

// Result is the "data" of a successful response.
type Result map[string]any

func decode(args json.RawMessage, into any) error {
	if len(args) == 0 {
		return nil
	}
	if err := json.Unmarshal(args, into); err != nil {
		return fmt.Errorf("bad arguments: %w", err)
	}
	return nil
}

func utf8(b []byte) string { return strings.ToValidUTF8(string(b), "�") }
