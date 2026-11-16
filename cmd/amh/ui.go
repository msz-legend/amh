package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"net/http"
	"strings"

	"amh/internal/webapi"
)

// runUI serves the localhost-only management web UI.
func runUI(args []string) error {
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:7788", "listen address (localhost only)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Hard-guarantee the loopback binding: whatever the user passes,
	// the host part is forced to 127.0.0.1.
	host, port, err := net.SplitHostPort(*addr)
	if err != nil {
		return fmt.Errorf("bad --addr %q: %w", *addr, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	listenAddr := net.JoinHostPort(host, port)

	db, mirrorDir, closeDB, err := openStore()
	if err != nil {
		return err
	}
	defer closeDB()

	token, err := newToken()
	if err != nil {
		return err
	}
	handler := webapi.New(webapi.Config{DB: db, MirrorDir: mirrorDir, Token: token})

	url := fmt.Sprintf("http://%s/?token=%s", listenAddr, token)
	fmt.Println("amh ui running (localhost only):", url)
	fmt.Println("  Ctrl+C to stop. The token is required for every API call.")
	return http.ListenAndServe(listenAddr, handler)
}

// newToken returns a random 128-bit hex token.
func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToLower(hex.EncodeToString(b)), nil
}
