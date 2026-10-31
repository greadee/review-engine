package exec

import (
	"context"
	"os/exec"
)

func Run(ctx context.Context, userInput string) error {
	return exec.CommandContext(ctx, "echo", userInput).Run()
}
