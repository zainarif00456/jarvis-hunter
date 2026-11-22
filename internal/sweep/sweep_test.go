package sweep

import "testing"

func TestIdentityHasTheBasics(t *testing.T) {
	id := Identity(true, "t")
	for _, k := range []string{"os", "arch", "user", "hostname", "os_version", "uptime_s", "is_admin", "addresses", "agent_version"} {
		if _, ok := id[k]; !ok {
			t.Errorf("identity is missing %q: %v", k, id)
		}
	}
	if id["user"] == "" || id["hostname"] == "" {
		t.Errorf("user/hostname empty: %v", id)
	}
}

func TestSystemAddsHardware(t *testing.T) {
	s := System(false, "t")
	if _, ok := s["mem_total_mb"]; !ok {
		t.Errorf("no memory info: %v", s)
	}
}
