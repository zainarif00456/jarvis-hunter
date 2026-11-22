//go:build windows

package sweep

import "golang.org/x/sys/windows"

// IsAdmin reports whether the process token is elevated.
func IsAdmin() bool { return windows.GetCurrentProcessToken().IsElevated() }

// CanElevate reports whether the user is in the Administrators group (UAC can then elevate).
func CanElevate() bool {
	var sid *windows.SID
	if err := windows.AllocateAndInitializeSid(&windows.SECURITY_NT_AUTHORITY, 2,
		windows.SECURITY_BUILTIN_DOMAIN_RID, windows.DOMAIN_ALIAS_RID_ADMINS, 0, 0, 0, 0, 0, 0, &sid); err != nil {
		return false
	}
	defer windows.FreeSid(sid)
	member, err := windows.Token(0).IsMember(sid)
	return err == nil && member
}
