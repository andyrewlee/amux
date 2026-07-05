// Package pprofhttp builds the opt-in pprof server used by amux binaries.
package pprofhttp

import (
	"net/http"
	"net/http/pprof"
	"strconv"
	"strings"
	"time"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 5 * time.Minute
	idleTimeout       = 1 * time.Minute
)

// AddrFromEnvValue converts an AMUX_PPROF value into a listen address.
func AddrFromEnvValue(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	lower := strings.ToLower(raw)
	switch lower {
	case "0", "false", "no":
		return "", false
	case "1", "true":
		return "127.0.0.1:6060", true
	}
	if _, err := strconv.Atoi(raw); err == nil {
		return "127.0.0.1:" + raw, true
	}
	return raw, true
}

// NewServer returns a pprof-only HTTP server with bounded timeouts.
func NewServer(addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           newMux(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}
