package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ravi-arnan/tenax/agent"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	agt, err := agent.New()
	if err != nil {
		slog.Error("agent init failed", "error", err)
		os.Exit(1)
	}

	if err := agt.Start(); err != nil {
		slog.Error("agent start failed", "error", err)
		os.Exit(1)
	}

	slog.Info("tenax agent started")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	slog.Info("shutting down")
	if err := agt.Stop(); err != nil {
		slog.Error("agent stop", "error", err)
	}
	fmt.Println("tenax stopped")
}
