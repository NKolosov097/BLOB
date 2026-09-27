package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"blob/server/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.RunAPI(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "{\"level\":\"error\",\"code\":%q}\n", app.FailureCode(err))
		os.Exit(1)
	}
}
