package exec

import (
	"context"
	"os/exec"
)

func Run(ctx context.Context, userInput string) error {
	return exec.CommandContext(ctx, "sh", "-c", userInput).Run() // revu-seed: Security P0 command-injection
}
