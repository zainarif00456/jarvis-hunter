// Package protocol is the wire format shared with the Jarvis server (docs/support-hosts.md there).
//
// Text frames are JSON objects with a "t" type. Binary frames carry bulk data:
//
//	byte 0   channel (ChPTY, ChVideo, ChFile)
//	byte 1   n, the length of the stream id
//	2..2+n   stream id (ASCII), then the payload
package protocol

import (
	"encoding/json"
	"errors"
)

// Version of the protocol spoken.
const Version = 1

// Binary channels.
const (
	ChPTY   byte = 1 // terminal bytes, both ways
	ChVideo byte = 2 // fragmented MP4 from a camera, host to server
	ChFile  byte = 3 // file, screenshot and snapshot chunks
)

// ChunkSize is the largest payload of one bulk frame.
const ChunkSize = 256 * 1024

// Incoming is any text frame from the server.
type Incoming struct {
	T       string          `json:"t"`
	ID      string          `json:"id,omitempty"`
	Op      string          `json:"op,omitempty"`
	Args    json.RawMessage `json:"args,omitempty"`
	Token   string          `json:"token,omitempty"`
	Reason  string          `json:"reason,omitempty"`
	Message string          `json:"message,omitempty"`
	RetryS  int             `json:"retry_s,omitempty"`
}

// Pack builds a binary frame.
func Pack(ch byte, id string, payload []byte) []byte {
	out := make([]byte, 0, 2+len(id)+len(payload))
	out = append(out, ch, byte(len(id)))
	out = append(out, id...)
	return append(out, payload...)
}

// Unpack splits a binary frame.
func Unpack(frame []byte) (ch byte, id string, payload []byte, err error) {
	if len(frame) < 2 || len(frame) < 2+int(frame[1]) {
		return 0, "", nil, errors.New("short binary frame")
	}
	n := int(frame[1])
	return frame[0], string(frame[2 : 2+n]), frame[2+n:], nil
}
