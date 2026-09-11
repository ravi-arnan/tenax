package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	cmd := &cobra.Command{
		Use:   "tenaxctl",
		Short: "Tenax CLI — interact with the tenax daemon",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("tenaxctl status — not yet connected to daemon")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "units",
		Short: "List systemd units managed by tenax",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("tenaxctl units — not yet connected to daemon")
			return nil
		},
	})

	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
