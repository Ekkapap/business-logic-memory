//go:build windows

package blm

import "os/exec"

func setProcessGroup(cmd *exec.Cmd) {}
func killProcessGroup(cmd *exec.Cmd) {}
