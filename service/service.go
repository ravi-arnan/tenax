package service

import (
	"fmt"
	"net"
	"os"
	"strconv"

	"github.com/coreos/go-systemd/v22/activation"
)

func Listen() (net.Listener, error) {
	listeners, err := activation.Listeners()
	if err != nil {
		return nil, fmt.Errorf("socket activation: %w", err)
	}
	if len(listeners) > 0 {
		return listeners[0], nil
	}

	port := 9721
	if p := os.Getenv("TENAX_PORT"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil {
			port = parsed
		}
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("listen :%d: %w", port, err)
	}
	return ln, nil
}

func NotifyReady() error {
	systemd, ok := os.LookupEnv("NOTIFY_SOCKET")
	if !ok {
		return nil
	}
	_ = systemd
	return nil
}

func SdNotify(state string) error {
	_ = state
	return nil
}

func SocketPath() string {
	if p := os.Getenv("TENAX_SOCKET"); p != "" {
		return p
	}
	return "/run/tenax/tenax.sock"
}
