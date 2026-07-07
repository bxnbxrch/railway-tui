package ui

import (
	"os/exec"
	"runtime"
)

// openURL opens a URL in the user's default browser. It returns any error from
// launching the opener (the browser process itself is detached). This lets the
// TUI open a service URL or the Railway project dashboard without relying on
// `railway open`, which only targets the *linked* project.
func openURL(url string) error {
	if url == "" {
		return nil
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// The empty first argument is the window title `start` expects, so a URL
		// containing spaces/ampersands isn't misparsed as the title.
		cmd = exec.Command("cmd", "/c", "start", "", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// dashboardURL builds the Railway web dashboard URL for a project (optionally
// deep-linked to an environment), matching what the desktop web app shows.
func dashboardURL(projectID, environmentID string) string {
	if projectID == "" {
		return "https://railway.com/dashboard"
	}
	u := "https://railway.com/project/" + projectID
	if environmentID != "" {
		u += "?environmentId=" + environmentID
	}
	return u
}
