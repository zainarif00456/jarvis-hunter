package camera

import (
	"strings"
	"testing"
)

func TestParseAVFoundationFindsWebcamFirstAndSkipsScreens(t *testing.T) {
	text := `[AVFoundation indev @ 0x1] AVFoundation video devices:
[AVFoundation indev @ 0x1] [0] Logitech BRIO
[AVFoundation indev @ 0x1] [1] FaceTime HD Camera
[AVFoundation indev @ 0x1] [2] Capture screen 0
[AVFoundation indev @ 0x1] AVFoundation audio devices:
[AVFoundation indev @ 0x1] [0] MacBook Pro Microphone`
	d := ParseAVFoundation(text)
	if len(d) != 2 || d[0].Name != "FaceTime HD Camera" || d[0].Kind != "builtin" || d[0].ID != "1" || d[1].Kind != "usb" {
		t.Fatalf("got %+v", d)
	}
}

func TestParseDShow(t *testing.T) {
	text := `[dshow @ 0x1] DirectShow video devices (some may be both video and audio devices)
[dshow @ 0x1]  "Integrated Camera" (video)
[dshow @ 0x1]     Alternative name "@device_pnp_..."
[dshow @ 0x1]  "Microphone Array" (audio)`
	d := ParseDShow(text)
	if len(d) != 1 || d[0].ID != "Integrated Camera" || d[0].Kind != "builtin" {
		t.Fatalf("got %+v", d)
	}
}

func TestRelayURLKeepsLoginOutOfPathAndEscapesIt(t *testing.T) {
	u, err := RelayURL(struct {
		URL      string `json:"url"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Path     string `json:"path"`
		Username string `json:"username"`
		Password string `json:"password"`
	}{Host: "192.168.1.40", Port: 554, Path: "/Streaming/Channels/101", Username: "admin", Password: "p@ss:w/rd"})
	if err != nil || !strings.HasPrefix(u, "rtsp://admin:p%40ss%3Aw%2Frd@192.168.1.40:554/Streaming") {
		t.Fatalf("got %q %v", u, err)
	}
}

func TestInputArgsForFakeAndMissing(t *testing.T) {
	if a, err := InputArgs(source{Device: "fake"}); err != nil || !strings.Contains(strings.Join(a, " "), "testsrc2") {
		t.Fatalf("fake: %v %v", a, err)
	}
	if _, err := InputArgs(source{}); err == nil {
		t.Fatal("no camera chosen should be an error")
	}
}
