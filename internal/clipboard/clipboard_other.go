//go:build !windows

package clipboard

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
)

// read asks the desktop's own clipboard tool.
func read() (string, error) {
	tools := [][]string{{"xclip", "-selection", "clipboard", "-o"}, {"xsel", "--clipboard", "--output"}}
	switch {
	case runtime.GOOS == "darwin":
		tools = [][]string{{"pbpaste"}}
	case os.Getenv("WAYLAND_DISPLAY") != "":
		tools = append([][]string{{"wl-paste", "--no-newline"}}, tools...)
	}
	found := false
	for _, t := range tools {
		path, err := exec.LookPath(t[0])
		if err != nil {
			continue
		}
		found = true
		if out, err := exec.Command(path, t[1:]...).Output(); err == nil {
			return string(out), nil
		}
	}
	if !found {
		return "", errors.New("pasting needs " + tools[0][0] + " installed")
	}
	return "", nil // the tools fail on an empty clipboard
}
