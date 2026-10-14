package exec

import "os/exec"

func Run(userInput string) {
	_ = exec.Command("sh", "-c", userInput).Run() // revu-seed: Security P0 command-injection
}
