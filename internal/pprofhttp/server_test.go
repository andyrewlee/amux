package pprofhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAddrFromEnvValue(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		allowRemote string
		wantAddr    string
		wantOK      bool
		wantWarn    bool
	}{
		{name: "empty", raw: "", wantOK: false},
		{name: "spaces", raw: " \t ", wantOK: false},
		{name: "false", raw: "false", wantOK: false},
		{name: "no", raw: "NO", wantOK: false},
		{name: "true", raw: "true", wantAddr: "127.0.0.1:6060", wantOK: true},
		{name: "one", raw: "1", wantAddr: "127.0.0.1:6060", wantOK: true},
		{name: "port", raw: "6060", wantAddr: "127.0.0.1:6060", wantOK: true},
		{name: "other port", raw: "7070", wantAddr: "127.0.0.1:7070", wantOK: true},
		{name: "loopback address", raw: "127.0.0.1:9090", wantAddr: "127.0.0.1:9090", wantOK: true},
		{name: "loopback nonstandard", raw: "127.0.0.1:7000", wantAddr: "127.0.0.1:7000", wantOK: true},
		{name: "ipv6 loopback", raw: "[::1]:6060", wantAddr: "[::1]:6060", wantOK: true},
		{
			name:     "all interfaces downgraded",
			raw:      "0.0.0.0:6060",
			wantAddr: "127.0.0.1:6060",
			wantOK:   true,
			wantWarn: true,
		},
		{
			name:     "empty host downgraded",
			raw:      ":6060",
			wantAddr: "127.0.0.1:6060",
			wantOK:   true,
			wantWarn: true,
		},
		{
			name:     "routable host downgraded",
			raw:      "192.168.1.5:6060",
			wantAddr: "127.0.0.1:6060",
			wantOK:   true,
			wantWarn: true,
		},
		{
			name:     "remote without port refused",
			raw:      "192.168.1.5",
			wantOK:   false,
			wantWarn: true,
		},
		{
			name:        "all interfaces allowed with opt-in",
			raw:         "0.0.0.0:6060",
			allowRemote: "1",
			wantAddr:    "0.0.0.0:6060",
			wantOK:      true,
			wantWarn:    true,
		},
		{
			name:        "empty host allowed with opt-in",
			raw:         ":6060",
			allowRemote: "1",
			wantAddr:    ":6060",
			wantOK:      true,
			wantWarn:    true,
		},
		{
			name:        "opt-in does not change loopback",
			raw:         "127.0.0.1:9090",
			allowRemote: "1",
			wantAddr:    "127.0.0.1:9090",
			wantOK:      true,
		},
		{
			name:        "opt-in zero is not an opt-in",
			raw:         "0.0.0.0:6060",
			allowRemote: "0",
			wantAddr:    "127.0.0.1:6060",
			wantOK:      true,
			wantWarn:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(AllowRemoteEnv, tt.allowRemote)
			gotAddr, gotOK, gotWarning := AddrFromEnvValue(tt.raw)
			if gotOK != tt.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotAddr != tt.wantAddr {
				t.Fatalf("addr = %q, want %q", gotAddr, tt.wantAddr)
			}
			if (gotWarning != "") != tt.wantWarn {
				t.Fatalf("warning = %q, want warn = %v", gotWarning, tt.wantWarn)
			}
		})
	}
}

func TestIsRemoteBind(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want bool
	}{
		{name: "ipv4 loopback", addr: "127.0.0.1:6060", want: false},
		{name: "ipv4 loopback range", addr: "127.0.0.2:6060", want: false},
		{name: "ipv6 loopback", addr: "[::1]:6060", want: false},
		{name: "all interfaces", addr: "0.0.0.0:6060", want: true},
		{name: "empty host binds all interfaces", addr: ":6060", want: true},
		{name: "routable", addr: "192.168.1.5:6060", want: true},
		{name: "hostname is not a literal loopback IP", addr: "localhost:6060", want: true},
		{name: "unparseable fails safe", addr: "not-an-address", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRemoteBind(tt.addr); got != tt.want {
				t.Fatalf("isRemoteBind(%q) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestNewServerConfiguresTimeouts(t *testing.T) {
	server := NewServer("127.0.0.1:0")
	if server.Addr != "127.0.0.1:0" {
		t.Fatalf("addr = %q, want 127.0.0.1:0", server.Addr)
	}
	if server.ReadHeaderTimeout != readHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", server.ReadHeaderTimeout, readHeaderTimeout)
	}
	if server.ReadTimeout != readTimeout {
		t.Fatalf("ReadTimeout = %s, want %s", server.ReadTimeout, readTimeout)
	}
	if server.WriteTimeout != writeTimeout {
		t.Fatalf("WriteTimeout = %s, want %s", server.WriteTimeout, writeTimeout)
	}
	if server.IdleTimeout != idleTimeout {
		t.Fatalf("IdleTimeout = %s, want %s", server.IdleTimeout, idleTimeout)
	}
	if server.Handler == nil {
		t.Fatal("handler is nil")
	}
}

func TestNewServerUsesPprofOnlyMux(t *testing.T) {
	server := NewServer("127.0.0.1:0")

	pprofReq := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	pprofResp := httptest.NewRecorder()
	server.Handler.ServeHTTP(pprofResp, pprofReq)
	if pprofResp.Code != http.StatusOK {
		t.Fatalf("pprof status = %d, want %d", pprofResp.Code, http.StatusOK)
	}

	otherReq := httptest.NewRequest(http.MethodGet, "/", nil)
	otherResp := httptest.NewRecorder()
	server.Handler.ServeHTTP(otherResp, otherReq)
	if otherResp.Code != http.StatusNotFound {
		t.Fatalf("root status = %d, want %d", otherResp.Code, http.StatusNotFound)
	}
}
