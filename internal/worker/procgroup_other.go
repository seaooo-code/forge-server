//go:build !unix

package worker

import "os/exec"

func detachProcessGroup(*exec.Cmd) {}
