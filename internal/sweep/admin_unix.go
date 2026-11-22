//go:build !windows

package sweep

import (
	"os"
	"os/user"
)

// IsAdmin reports whether the app runs as root.
func IsAdmin() bool { return os.Geteuid() == 0 }

// CanElevate reports whether the user belongs to a group that may use sudo (admin, sudo, wheel).
func CanElevate() bool {
	if IsAdmin() {
		return true
	}
	u, err := user.Current()
	if err != nil {
		return false
	}
	ids, err := u.GroupIds()
	if err != nil {
		return false
	}
	for _, id := range ids {
		if g, err := user.LookupGroupId(id); err == nil {
			switch g.Name {
			case "admin", "sudo", "wheel":
				return true
			}
		}
	}
	return false
}
