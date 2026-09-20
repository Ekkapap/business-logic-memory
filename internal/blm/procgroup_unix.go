//go:build !windows

package blm

import (
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// killProcessGroup ฆ่าทั้งกลุ่ม (npx → node → …) — เจ้าของ 2026-09-20: Ctrl-C ที่ blm แล้ว socraticode ยัง index ต่อเป็น orphan
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
