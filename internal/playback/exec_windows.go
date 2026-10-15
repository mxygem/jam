//go:build windows

package playback

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps ffmpeg from flashing a console window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
