// Command healthcheck probes the server's /healthz endpoint and exits 0
// when it answers 200. It exists because the distroless runtime image has
// no shell, curl, or wget, so the Docker healthcheck cannot be a shell
// snippet; this tiny static binary is built alongside the server and copied
// into the runtime image as the healthcheck command.
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		fatal("bad LISTEN_ADDR %q: %v", addr, err)
	}
	switch host {
	case "", "0.0.0.0", "::": // wildcard listeners also answer on loopback
		host = "127.0.0.1"
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		fatal("healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		fatal("healthz: status %d", resp.StatusCode)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "healthcheck: "+format+"\n", args...)
	os.Exit(1)
}
