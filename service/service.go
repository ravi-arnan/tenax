package service

import (
	"fmt"
	"net"
	"os"
	"strconv"

	"github.com/coreos/go-systemd/v22/activation"
)

// Listen returns the listener for the tenax API. Precedence:
//
//  1. explicit addr (from config), e.g. "127.0.0.1:9721"
//  2. systemd socket activation (LISTEN_FDS)
//  3. TENAX_PORT env var
//  4. default 127.0.0.1:9721
func Listen(addr string) (net.Listener, error) {
	if addr != "" {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("listen %s: %w", addr, err)
		}
		return ln, nil
	}

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

func SocketPath() string {
	if p := os.Getenv("TENAX_SOCKET"); p != "" {
		return p
	}
	return "/run/tenax/tenax.sock"
}
