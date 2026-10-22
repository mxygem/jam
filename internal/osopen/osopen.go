// Package osopen hands a file or folder to the system: a folder opens in the
// file browser, a file in its default app (a report in the web browser).
package osopen

import (
	"os/exec"
	"runtime"
)

// Open starts the system's handler for path and returns without waiting.
func Open(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
