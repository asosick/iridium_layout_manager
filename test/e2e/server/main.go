// Command server runs the layout manager's E2E test application (see
// internal/e2eapp) on 127.0.0.1:18788, or on E2E_PORT when set. Each
// Playwright worker starts its own copy on its own port. With E2E_TLS set it
// serves HTTPS with a throwaway self-signed certificate: WebKit refuses the
// plugin's Secure cookies over plain HTTP, even on loopback. Used exclusively
// by the Playwright specs. Not for production: every request is signed in
// and the /e2e endpoints take no credentials, so it listens on loopback only.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"time"

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
	scheme, serve := "http", server.Serve
	if os.Getenv("E2E_TLS") != "" {
		cert, err := selfSigned()
		if err != nil {
			log.Fatal(err)
		}
		server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
		scheme = "https"
		serve = func(l net.Listener) error { return server.ServeTLS(l, "", "") }
	}
	fmt.Printf("layout manager E2E server listening on %s://127.0.0.1:%s%s/%s\n", scheme, port, e2eapp.PanelPath, e2eapp.PageDefault)
	for _, listener := range listeners[1:] {
		go func() { log.Fatal(serve(listener)) }()
	}
	log.Fatal(serve(listeners[0]))
}

// selfSigned makes a certificate for the loopback addresses, valid for a day.
func selfSigned() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "layout-manager-e2e"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
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
