package upgrade

import (
	"os/exec"
	"time"
)

func boundCommand(c *exec.Cmd) { c.WaitDelay = 5 * time.Second }
