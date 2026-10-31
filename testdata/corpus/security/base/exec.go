package exec

import "os/exec"

func Run(userInput string) error {
	return exec.Command("echo", userInput).Run()
}
