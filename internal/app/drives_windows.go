//go:build windows

package app

import "syscall"

// drives lists the drive roots that exist ("C:\", "E:\"...).
func drives() []string {
	mask, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetLogicalDrives").Call()
	var out []string
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) != 0 {
			out = append(out, string(rune('A'+i))+`:\`)
		}
	}
	return out
}
