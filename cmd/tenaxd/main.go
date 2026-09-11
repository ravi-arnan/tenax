package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ravi-arnan/tenax/agent"
	"github.com/ravi-arnan/tenax/tui"
	"github.com/spf13/cobra"
)

func main() {
	cmd := &cobra.Command{
		Use:   "tenaxd",
		Short: "Tenax daemon — Linux server control plane agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemon()
		},
		SilenceUsage: true,
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "tui",
		Short: "Start the terminal dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Print daemon status summary",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus()
		},
	})

	verbose := cmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")

	cmd.PersistentPreRun = func(cmdCobra *cobra.Command, args []string) {
		if *verbose {
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})))
		}
	}

	if err := cmd.Execute(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func runDaemon() error {
	slog.Info("tenaxd starting")

	agt, err := agent.New()
	if err != nil {
		return fmt.Errorf("agent init: %w", err)
	}

	if err := agt.Start(); err != nil {
		return fmt.Errorf("agent start: %w", err)
	}

	slog.Info("tenaxd running", "units", len(agt.Units()))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	<-ctx.Done()

	slog.Info("shutting down")
	return agt.Stop()
}

func runTUI() error {
	agt, err := agent.New()
	if err != nil {
		return fmt.Errorf("agent init: %w", err)
	}

	if err := agt.Start(); err != nil {
		return fmt.Errorf("agent start: %w", err)
	}
	defer agt.Stop()

	p := tea.NewProgram(tui.NewDashboard(agt), tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func runStatus() error {
	agt, err := agent.New()
	if err != nil {
		return fmt.Errorf("agent init: %w", err)
	}

	if err := agt.Start(); err != nil {
		return fmt.Errorf("agent start: %w", err)
	}
	defer agt.Stop()

	units := agt.Units()
	active := 0
	failed := 0
	for _, u := range units {
		switch u.ActiveState {
		case "active":
			active++
		case "failed":
			failed++
		}
	}

	fmt.Printf("tenaxd status\n")
	fmt.Printf("  host:  %s\n", hostname())
	fmt.Printf("  units: %d total, %d active, %d failed\n", len(units), active, failed)
	return nil
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}
