// Package version holds the application version.
//
// The single source of truth is `info.version` in build/config.yml. The
// Taskfile builds stamp it in at link time:
//
//	go build -ldflags "-X github.com/solean/ponder/internal/version.Version=1.2.3"
//
// Builds that skip the Taskfile (go run, go test, the air dev server) keep the
// Unstamped value.
package version

// Unstamped is the version of a build that did not go through the Taskfile.
const Unstamped = "0.0.0-dev"

// Version is the application version, set at link time.
var Version = Unstamped

// UserAgent returns the User-Agent for outbound HTTP requests, such as those
// to Scryfall and 17Lands. Those APIs ask clients to identify themselves.
func UserAgent(purpose string) string {
	return "ponder/" + Version + " (" + purpose + ")"
}
