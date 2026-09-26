//go:build !unix

package ai

import "os/exec"

func setProcessGroup(*exec.Cmd) {}

func killProcessGroup(*exec.Cmd) {}
