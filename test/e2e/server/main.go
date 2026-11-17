// Command server runs the layout manager's E2E test application (see
// internal/e2eapp) on 127.0.0.1:18788, or on E2E_PORT when set. Each
// Playwright worker starts its own copy on its own port. Used exclusively by
// the Playwright specs. Not for production: every request is signed in and
// the /e2e endpoints take no credentials, so it listens on loopback only.
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/asosick/iridium_layout_manager/internal/e2eapp"
)

const defaultPort = "18788"

func main() {
	app := e2eapp.New()

	port := os.Getenv("E2E_PORT")
	if port == "" {
		port = defaultPort
	}
	listeners, err := loopbackListeners(port)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Handler: app.Mux}
	fmt.Printf("layout manager E2E server listening on http://127.0.0.1:%s%s/%s\n", port, e2eapp.PanelPath, e2eapp.PageDefault)
	for _, listener := range listeners[1:] {
		go func() { log.Fatal(server.Serve(listener)) }()
	}
	log.Fatal(server.Serve(listeners[0]))
}

// loopbackListeners listens on port on the IPv4 loopback address and, when
// the machine has one, on the IPv6 loopback, which "localhost" may resolve to.
func loopbackListeners(port string) ([]net.Listener, error) {
	v4, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return nil, err
	}
	listeners := []net.Listener{v4}
	if v6, err := net.Listen("tcp", net.JoinHostPort("::1", port)); err == nil {
		listeners = append(listeners, v6)
	}
	return listeners, nil
}
