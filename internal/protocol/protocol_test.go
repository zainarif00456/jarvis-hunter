package protocol

import (
	"bytes"
	"testing"
)

func TestPackUnpackRoundTrip(t *testing.T) {
	frame := Pack(ChFile, "abc123", []byte("payload"))
	ch, id, payload, err := Unpack(frame)
	if err != nil || ch != ChFile || id != "abc123" || !bytes.Equal(payload, []byte("payload")) {
		t.Fatalf("got %v %q %q %v", ch, id, payload, err)
	}
}

func TestUnpackRejectsShortFrames(t *testing.T) {
	for _, f := range [][]byte{nil, {1}, {1, 5, 'a'}} {
		if _, _, _, err := Unpack(f); err == nil {
			t.Errorf("frame %v should be rejected", f)
		}
	}
}

func TestEmptyPayloadAndID(t *testing.T) {
	_, id, payload, err := Unpack(Pack(ChPTY, "", nil))
	if err != nil || id != "" || len(payload) != 0 {
		t.Fatal("empty frame should round trip")
	}
}
