package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ravi-arnan/tenax/agent"
	"github.com/ravi-arnan/tenax/api"
	"github.com/ravi-arnan/tenax/config"
	"github.com/ravi-arnan/tenax/probe"
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

	configPath := cmd.PersistentFlags().String("config", "", "config file (default: /etc/tenax/tenax.yaml)")
	verbose := cmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")

	cmd.PersistentPreRun = func(cmdCobra *cobra.Command, args []string) {
		if *verbose {
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})))
		}
		if *configPath != "" {
			os.Setenv("TENAX_CONFIG", *configPath)
		}
	}

	if err := cmd.Execute(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func loadConfig() (*config.Config, error) {
	if p := os.Getenv("TENAX_CONFIG"); p != "" {
		cfg, err := config.LoadFile(p)
		if err != nil {
			return nil, fmt.Errorf("config %s: %w", p, err)
		}
		return cfg, nil
	}
	return config.Load()
}

func runDaemon() error {
	slog.Info("tenaxd starting")

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	agt, err := agent.New(cfg)
	if err != nil {
		return fmt.Errorf("agent init: %w", err)
	}

	if err := agt.Start(); err != nil {
		return fmt.Errorf("agent start: %w", err)
	}

	apiSrv, err := api.NewServer(agt, agt, cfg.API.Listen)
	if err != nil {
		slog.Warn("api server disabled", "error", err)
	} else {
		if err := apiSrv.Start(); err != nil {
			return fmt.Errorf("api start: %w", err)
		}
		slog.Info("api server listening", "addr", apiSrv.Addr())
	}

	slog.Info("tenaxd running", "units", len(agt.Units()))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	<-ctx.Done()

	slog.Info("shutting down")
	if apiSrv != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := apiSrv.Stop(shutdownCtx); err != nil {
			slog.Warn("api shutdown", "error", err)
		}
	}
	return agt.Stop()
}

func runTUI() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	agt, err := agent.New(cfg)
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
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	agt, err := agent.New(cfg)
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

	if probes := agt.Probes(); len(probes) > 0 {
		healthy, down := 0, 0
		for _, p := range probes {
			switch p.Status {
			case probe.StatusHealthy:
				healthy++
			case probe.StatusDown, probe.StatusDegraded:
				down++
			}
		}
		fmt.Printf("  probes: %d total, %d healthy, %d unhealthy\n", len(probes), healthy, down)
		for _, p := range probes {
			marker := "ok"
			if p.Status != probe.StatusHealthy {
				marker = "FAIL"
			}
			fmt.Printf("    [%s] %-8s %s (%s)\n", marker, p.Probe.Type, p.Probe.Name, p.Status)
		}
	}
	return nil
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}
