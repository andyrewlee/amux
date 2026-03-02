package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/andyrewlee/medusa/internal/server"
	"github.com/andyrewlee/medusa/internal/service"
)

func main() {
	port := flag.Int("port", 8420, "HTTP port")
	bind := flag.String("bind", "0.0.0.0", "Bind address")
	tlsCert := flag.String("tls-cert", "", "TLS certificate file")
	tlsKey := flag.String("tls-key", "", "TLS key file")
	flag.Parse()

	// Initialize logging
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".medusa", "logs")
	if err := logging.Initialize(logDir, logging.LevelDebug); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not initialize logging: %v\n", err)
	}
	defer logging.Close()

	// Load config
	cfg, err := config.DefaultConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize services
	svc, err := service.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize services: %v\n", err)
		os.Exit(1)
	}

	// Determine token directory
	tokenDir := filepath.Dir(cfg.Paths.RegistryPath)

	// Create and start server
	srv, err := server.New(server.Config{
		Port:     *port,
		Bind:     *bind,
		TLSCert:  *tlsCert,
		TLSKey:   *tlsKey,
		TokenDir: tokenDir,
	}, svc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create server: %v\n", err)
		os.Exit(1)
	}

	// Print connection info
	scheme := "http"
	if *tlsCert != "" {
		scheme = "https"
	}
	fmt.Printf("Medusa server starting on %s://%s:%d\n", scheme, *bind, *port)
	fmt.Printf("Auth token: %s\n", srv.Token())
	fmt.Printf("Web UI: %s://localhost:%d\n", scheme, *port)

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.Start(); err != nil {
			logging.Warn("Server error: %v", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
}
