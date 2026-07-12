// Command kapo-web serves the browser multiplayer UI for Kapo.
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"

	"kapo/pkg/server"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	srv := server.New(logger)
	logger.Info("listening", "addr", *addr)
	if err := http.ListenAndServe(*addr, srv.Routes()); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}
