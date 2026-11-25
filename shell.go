package main

import (
	"context"
	"os/exec"
	"strings"
)

func shellCommand(ctx context.Context, args []string) error {
	shellName := ".#default"
	if len(args) > 0 {
		shellName = args[0]
	}

	if strings.HasPrefix(shellName, ".#") && strings.Contains(shellName, ":") {
		shellName = ".#" + shellName
	}

	cmd := exec.CommandContext(ctx, "", "")
	cmd.Run()

	return nil
}
