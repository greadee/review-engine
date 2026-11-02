package exec

import (
	"fmt"
	"os/exec"
)

func Run(userInput string) error {
	if err := exec.Command("sh", "-c", userInput).Run(); err != nil { // revu-seed: Security P0 command-injection
		return fmt.Errorf("run command: %w", err)
	}
	return nil
}
