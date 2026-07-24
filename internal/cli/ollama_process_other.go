//go:build !(linux || darwin)

package cli

import "os/exec"

func configureDetachedProcess(*exec.Cmd) {}
