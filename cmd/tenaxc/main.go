package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ravi-arnan/tenax/controller"
	"github.com/ravi-arnan/tenax/tui"
)

func main() {
	var (
		grpcAddr     string
		httpAddr     string
		livenessMins int
		verbose      bool
	)

	cmd := &cobra.Command{
		Use:   "tenaxc",
		Short: "Tenax controller — collects from multiple tenaxd agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			if verbose {
				slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
			}

			slog.Info("tenaxc starting")

			reg := controller.NewRegistry(time.Duration(livenessMins) * time.Minute)

			grpcLn, err := net.Listen("tcp", grpcAddr)
			if err != nil {
				return fmt.Errorf("grpc listen: %w", err)
			}
			httpLn, err := net.Listen("tcp", httpAddr)
			if err != nil {
				return fmt.Errorf("http listen: %w", err)
			}

			srv, err := controller.NewServer(reg, grpcLn, httpLn)
			if err != nil {
				return fmt.Errorf("controller init: %w", err)
			}

			if err := srv.Start(); err != nil {
				return fmt.Errorf("controller start: %w", err)
			}

			g, h := srv.Addrs()
			slog.Info("controller listening", "grpc", g, "http", h)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			<-ctx.Done()

			slog.Info("shutting down")
			return srv.Stop(context.Background())
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVar(&grpcAddr, "grpc-addr", ":9731", "gRPC listen address for agents")
	cmd.Flags().StringVar(&httpAddr, "http-addr", "127.0.0.1:9730", "HTTP listen address for UI/API")
	cmd.Flags().IntVar(&livenessMins, "liveness-minutes", 2, "host considered stale after this many minutes")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")

	cmd.AddCommand(&cobra.Command{
		Use:   "tui",
		Short: "Start the fleet terminal dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			if verbose {
				slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
			}

			reg := controller.NewRegistry(time.Duration(livenessMins) * time.Minute)

			grpcLn, err := net.Listen("tcp", grpcAddr)
			if err != nil {
				return fmt.Errorf("grpc listen: %w", err)
			}
			httpLn, err := net.Listen("tcp", httpAddr)
			if err != nil {
				return fmt.Errorf("http listen: %w", err)
			}

			srv, err := controller.NewServer(reg, grpcLn, httpLn)
			if err != nil {
				return fmt.Errorf("controller init: %w", err)
			}
			if err := srv.Start(); err != nil {
				return fmt.Errorf("controller start: %w", err)
			}

			fetch := func() tui.FleetData {
				now := time.Now()
				hosts := reg.List()
				out := tui.FleetData{}
				for _, h := range hosts {
					fh := tui.FleetHost{
						ID:          h.ID,
						Status:      h.StatusNow(now, reg.Deadline()),
						Tags:        h.Tags,
						UnitsTotal:  int(h.Status.GetUnitsTotal()),
						UnitsActive: int(h.Status.GetUnitsActive()),
						UnitsFailed: int(h.Status.GetUnitsFailed()),
						LastSeenAgo: now.Sub(h.LastSeen).Round(time.Second).String(),
					}
					if h.Latest != nil {
						fh.CPUPercent = h.Latest.CPUPercent
						fh.MemPercent = h.Latest.MemPercent
					}
					out.Hosts = append(out.Hosts, fh)
				}
				return out
			}

			_, _ = tea.NewProgram(tui.NewFleetModel(fetch), tea.WithAltScreen()).Run()
			return nil
		},
	})

	if err := cmd.Execute(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}
