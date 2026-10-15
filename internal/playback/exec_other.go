//go:build !windows

package playback

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
