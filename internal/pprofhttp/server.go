// Package pprofhttp builds the opt-in pprof server used by amux binaries.
package pprofhttp

import (
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"strings"
	"time"
)

// AllowRemoteEnv is the environment variable that opts in to serving pprof on
// a non-loopback address. Set it to "1" (or "true") alongside a non-loopback
// AMUX_PPROF value to allow the remote bind.
const AllowRemoteEnv = "AMUX_PPROF_ALLOW_REMOTE"

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 5 * time.Minute
	idleTimeout       = 1 * time.Minute
)

// AddrFromEnvValue converts an AMUX_PPROF value into a listen address.
//
// The convenience forms are loopback-locked: "1"/"true" resolve to
// 127.0.0.1:6060 and a bare port resolves to 127.0.0.1:<port>. An explicit
// host:port whose host is non-loopback — including an empty host such as
// ":6060", which binds all interfaces — is only honored when the
// AMUX_PPROF_ALLOW_REMOTE=1 opt-in is also set, because the pprof endpoints
// serve process argv and heap contents (which can include in-memory secrets)
// to anyone who can reach the port, with no authentication. Without the
// opt-in, a non-loopback bind with a usable port is downgraded to
// 127.0.0.1:<port>; one without a usable port is refused (ok=false).
//
// The returned warning is non-empty whenever a non-loopback bind was
// requested (allowed, downgraded, or refused); callers should log it.
func AddrFromEnvValue(raw string) (addr string, ok bool, warning string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, ""
	}
	lower := strings.ToLower(raw)
	switch lower {
	case "0", "false", "no":
		return "", false, ""
	case "1", "true":
		return "127.0.0.1:6060", true, ""
	}
	if _, err := strconv.Atoi(raw); err == nil {
		return "127.0.0.1:" + raw, true, ""
	}
	if !isRemoteBind(raw) {
		return raw, true, ""
	}
	if remoteAllowed() {
		return raw, true, "AMUX_PPROF=" + raw + " serves pprof on a non-loopback address with no auth; " +
			"profiles expose process argv and heap contents (" + AllowRemoteEnv + "=1 is set)"
	}
	if _, port, err := net.SplitHostPort(raw); err == nil && port != "" {
		fallback := "127.0.0.1:" + port
		return fallback, true, "AMUX_PPROF=" + raw + " requests a non-loopback bind; downgraded to " +
			fallback + " (set " + AllowRemoteEnv + "=1 to allow the remote bind)"
	}
	return "", false, "AMUX_PPROF=" + raw + " requests a non-loopback bind without a usable port; " +
		"pprof disabled (set " + AllowRemoteEnv + "=1 to allow the remote bind)"
}

// isRemoteBind reports whether addr asks for a bind on a non-loopback host.
// An empty host (":6060") binds all interfaces, so it counts as remote, as
// does any host that is not a literal loopback IP (127.0.0.0/8 or ::1).
// Unparseable addresses are treated as remote to stay fail-safe.
func isRemoteBind(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

// remoteAllowed reports whether the AMUX_PPROF_ALLOW_REMOTE opt-in is set.
func remoteAllowed() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(AllowRemoteEnv))) {
	case "1", "true":
		return true
	}
	return false
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
