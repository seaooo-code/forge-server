//go:build unix

package worker

import (
	"os/exec"
	"syscall"
)

// detachProcessGroup 让子进程（git、docker）进入独立的进程组。终端 Ctrl-C 或按进程组发送的 SIGINT/SIGTERM
// 只到达 worker 本身，不会直接中止进行中的构建；子进程的生命周期由 worker 的 context 控制，保证退出时能排空。
func detachProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
