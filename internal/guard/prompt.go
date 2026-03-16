package guard

// AutoDenyHandler returns a PermissionHandler that always denies (for non-interactive mode).
func AutoDenyHandler() PermissionHandler {
	return func(_ string, _ Decision) PermissionResult {
		return PermissionResult{Approved: false}
	}
}
