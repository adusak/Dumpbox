package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/adusak/Dumpbox/internal/dumpbox"
	"github.com/coreos/go-oidc/v3/oidc"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	config, err := dumpbox.LoadConfig()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	startupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	provider, err := oidc.NewProvider(startupContext, config.OIDCIssuer)
	cancel()
	if err != nil {
		logger.Error("discover OIDC provider", "error", err)
		os.Exit(1)
	}
	app, err := dumpbox.NewServer(config, provider, logger)
	if err != nil {
		logger.Error("initialize server", "error", err)
		os.Exit(1)
	}

	stop, cancelStop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancelStop()
	go app.RunCleanup(stop)

	server := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
	go func() {
		logger.Info("Dumpbox listening", "address", config.ListenAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve", "error", err)
			os.Exit(1)
		}
	}()

	<-stop.Done()
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		logger.Error("shutdown", "error", err)
	}
}
