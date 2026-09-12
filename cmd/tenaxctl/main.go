package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	defaultAPI        = "http://127.0.0.1:9721"
	defaultController = "http://127.0.0.1:9730"
)

func apiBase() string {
	if b := os.Getenv("TENAX_API"); b != "" {
		return strings.TrimRight(b, "/")
	}
	return defaultAPI
}

func controllerBase() string {
	if b := os.Getenv("TENAX_CONTROLLER"); b != "" {
		return strings.TrimRight(b, "/")
	}
	return defaultController
}

func apiGet(path string, out interface{}) error {
	return apiGetAt(apiBase(), path, out)
}

func apiGetAt(base, path string, out interface{}) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(base + path)
	if err != nil {
		return fmt.Errorf("connect to %s: %w (is the server running?)", base, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return json.Unmarshal(body, out)
}

func apiPost(path string) (map[string]interface{}, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(apiBase()+path, "application/json", nil)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w (is tenaxd running?)", apiBase(), err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var out map[string]interface{}
	json.Unmarshal(body, &out)

	if resp.StatusCode != http.StatusOK {
		if msg, ok := out["error"].(string); ok {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return out, nil
}

type unitsResponse struct {
	Units []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		ActiveState string `json:"active_state"`
		SubState    string `json:"sub_state"`
		LoadState   string `json:"load_state"`
		PID         int    `json:"pid"`
	} `json:"units"`
}

func main() {
	cmd := &cobra.Command{
		Use:   "tenaxctl",
		Short: "Tenax CLI — interact with the tenax daemon",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show daemon status summary",
		RunE: func(cmd *cobra.Command, args []string) error {
			var s struct {
				Hostname string `json:"hostname"`
				Units    int    `json:"units_total"`
				Active   int    `json:"units_active"`
				Failed   int    `json:"units_failed"`
				Probes   []struct {
					Name   string `json:"name"`
					Status string `json:"status"`
				} `json:"probes"`
				Memory struct {
					Percent float64 `json:"percent_used"`
				} `json:"memory"`
			}
			if err := apiGet("/api/status", &s); err != nil {
				return err
			}

			fmt.Printf("tenax @ %s\n", s.Hostname)
			fmt.Printf("  units:  %d total, %d active, %d failed\n", s.Units, s.Active, s.Failed)
			fmt.Printf("  memory: %.1f%%\n", s.Memory.Percent)
			if len(s.Probes) > 0 {
				healthy := 0
				for _, p := range s.Probes {
					if p.Status == "healthy" {
						healthy++
					}
				}
				fmt.Printf("  probes: %d/%d healthy\n", healthy, len(s.Probes))
				for _, p := range s.Probes {
					if p.Status != "healthy" {
						fmt.Printf("    [%s] %s\n", p.Status, p.Name)
					}
				}
			}
			return nil
		},
	})

	unitsCmd := &cobra.Command{
		Use:   "units",
		Short: "List systemd units tracked by tenax",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "/api/units"
			if state, _ := cmd.Flags().GetString("state"); state != "" {
				path += "?state=" + state
			}
			var resp unitsResponse
			if err := apiGet(path, &resp); err != nil {
				return err
			}
			for _, u := range resp.Units {
				marker := " "
				switch {
				case u.ActiveState == "failed":
					marker = "!"
				case u.ActiveState == "active":
					marker = "*"
				}
				sub := u.Description
				if sub == "" {
					sub = u.SubState
				}
				fmt.Printf("%s %-45s %-10s %s\n", marker, u.Name, u.ActiveState, sub)
			}
			fmt.Printf("\n%d units (* active, ! failed)\n", len(resp.Units))
			return nil
		},
	}
	unitsCmd.Flags().String("state", "", "filter by active state (active, failed, ...)")
	cmd.AddCommand(unitsCmd)

	for _, action := range []string{"start", "stop", "restart"} {
		action := action
		cmd.AddCommand(&cobra.Command{
			Use:   action + " <unit>",
			Short: action + " a systemd unit",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				out, err := apiPost("/api/units/" + args[0] + "/" + action)
				if err != nil {
					return err
				}
				fmt.Printf("%s %s: job %v\n", action, args[0], out["job"])
				return nil
			},
		})
	}

	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "Show recent journal entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			unit, _ := cmd.Flags().GetString("unit")
			limit, _ := cmd.Flags().GetInt("limit")

			q := fmt.Sprintf("/api/logs?limit=%d", limit)
			if unit != "" {
				q += "&unit=" + unit
			}

			var resp struct {
				Entries []struct {
					Timestamp time.Time `json:"ts"`
					Unit      string    `json:"unit"`
					Message   string    `json:"message"`
					Priority  int       `json:"priority"`
				} `json:"entries"`
			}
			if err := apiGet(q, &resp); err != nil {
				return err
			}
			for _, e := range resp.Entries {
				marker := " "
				if e.Priority <= 3 {
					marker = "!"
				}
				fmt.Printf("%s %s %-24s %s\n", marker, e.Timestamp.Format("15:04:05"), e.Unit, e.Message)
			}
			return nil
		},
	}
	logsCmd.Flags().String("unit", "", "filter by unit")
	logsCmd.Flags().Int("limit", 50, "max entries")
	cmd.AddCommand(logsCmd)

	historyCmd := &cobra.Command{
		Use:   "history",
		Short: "Show unit state transitions and recent resource samples",
		RunE: func(cmd *cobra.Command, args []string) error {
			var evResp struct {
				Events []struct {
					Unit string    `json:"unit"`
					From string    `json:"from_state"`
					To   string    `json:"to_state"`
					Time time.Time `json:"time"`
				} `json:"events"`
			}
			if err := apiGet("/api/history/units", &evResp); err != nil {
				return err
			}
			if len(evResp.Events) == 0 {
				fmt.Println("no unit events recorded yet")
			} else {
				fmt.Println("unit events:")
				start := len(evResp.Events) - 15
				if start < 0 {
					start = 0
				}
				for _, e := range evResp.Events[start:] {
					fmt.Printf("  %s %-40s %s -> %s\n", e.Time.Format("15:04:05"), e.Unit, e.From, e.To)
				}
			}

			var sResp struct {
				Samples []struct {
					Time   time.Time `json:"time"`
					CPUPct float64   `json:"cpu_percent"`
					MemPct float64   `json:"mem_percent"`
				} `json:"samples"`
			}
			if err := apiGet("/api/history/resources", &sResp); err != nil {
				return err
			}
			if len(sResp.Samples) > 0 {
				last := sResp.Samples[len(sResp.Samples)-1]
				fmt.Printf("resources: cpu %.1f%%, mem %.1f%% (%d samples)\n",
					last.CPUPct, last.MemPct, len(sResp.Samples))
			}
			return nil
		},
	}
	cmd.AddCommand(historyCmd)

	probesCmd := &cobra.Command{
		Use:   "probes",
		Short: "Show probe statuses",
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp struct {
				Probes []struct {
					Name   string `json:"name"`
					Type   string `json:"type"`
					Status string `json:"status"`
					Fails  int    `json:"consecutive_failures"`
				} `json:"probes"`
			}
			if err := apiGet("/api/probes", &resp); err != nil {
				return err
			}
			if len(resp.Probes) == 0 {
				fmt.Println("no probes configured")
				return nil
			}
			for _, p := range resp.Probes {
				fmt.Printf("%-24s %-6s %-8s fails=%d\n", p.Name, p.Type, p.Status, p.Fails)
			}
			return nil
		},
	}
	cmd.AddCommand(probesCmd)

	hostsCmd := &cobra.Command{
		Use:   "hosts",
		Short: "List hosts known to a tenaxc controller",
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp struct {
				Hosts []struct {
					ID          string `json:"id"`
					FleetStatus string `json:"fleet_status"`
					Version     string `json:"version"`
					OS          string `json:"os"`
					Kernel      string `json:"kernel"`
					Status      struct {
						UnitsTotal  int `json:"units_total"`
						UnitsActive int `json:"units_active"`
						UnitsFailed int `json:"units_failed"`
					} `json:"status"`
					Latest struct {
						CPUPercent float64 `json:"cpu_percent"`
						MemPercent float64 `json:"mem_percent"`
					} `json:"latest"`
				} `json:"hosts"`
			}
			if err := apiGetAt(controllerBase(), "/api/hosts", &resp); err != nil {
				return err
			}
			if len(resp.Hosts) == 0 {
				fmt.Println("no hosts registered")
				return nil
			}
			for _, h := range resp.Hosts {
				fmt.Printf("%-24s %-12s cpu %5.1f%% mem %5.1f%% units %d/%d failed=%d  %s %s\n",
					h.ID, h.FleetStatus, h.Latest.CPUPercent, h.Latest.MemPercent,
					h.Status.UnitsActive, h.Status.UnitsTotal, h.Status.UnitsFailed,
					h.OS, h.Kernel)
			}
			return nil
		},
	}
	cmd.AddCommand(hostsCmd)

	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
