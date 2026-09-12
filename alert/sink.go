package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// LogSink logs alert events via slog.
type LogSink struct{}

func (LogSink) Send(e Event) error {
	lvl := slog.LevelInfo
	if e.State == StateFiring {
		lvl = slog.LevelWarn
	}
	slog.Log(context.Background(), lvl, "alert", "rule", e.Rule, "kind", e.Kind, "message", e.Message)
	return nil
}

// WebhookSink POSTs alert events as JSON to a URL.
type WebhookSink struct {
	URL     string
	Client  *http.Client
	Timeout time.Duration
}

func (w WebhookSink) Send(e Event) error {
	timeout := w.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	client := w.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	body, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	resp, err := client.Post(w.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook %s: %w", w.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook %s: status %d", w.URL, resp.StatusCode)
	}
	return nil
}

// StdoutSink prints events to stdout (useful for foreground runs).
type StdoutSink struct{}

func (StdoutSink) Send(e Event) error {
	fmt.Fprintf(os.Stdout, "ALERT [%s] %s: %s\n", e.State, e.Rule, e.Message)
	return nil
}
