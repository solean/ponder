package appstate

// LoginLaunchFlag is passed by the launch-at-login agent so the app can tell a
// login launch from one the user started by hand.
const LoginLaunchFlag = "--launched-at-login"

// LaunchedAtLogin reports whether args (without the program name) carry the
// login launch flag.
func LaunchedAtLogin(args []string) bool {
	for _, arg := range args {
		if arg == LoginLaunchFlag {
			return true
		}
	}
	return false
}

// AutostartStatus describes whether the app is configured to launch at login.
type AutostartStatus struct {
	Supported  bool   `json:"supported"`
	Enabled    bool   `json:"enabled"`
	AgentPath  string `json:"agentPath,omitempty"`
	Executable string `json:"executable,omitempty"`
	Note       string `json:"note,omitempty"`
}
