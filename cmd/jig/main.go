// Command jig is the entry point for the jig CLI.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/gammons/jig/internal/app"
)

func main() {
	reexecBare()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := app.Run(ctx, os.Args[1:], app.Stdio{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}, os.Getenv)
	stop()
	os.Exit(code)
}
