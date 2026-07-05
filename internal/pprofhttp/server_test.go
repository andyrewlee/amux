package pprofhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAddrFromEnvValue(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantAddr string
		wantOK   bool
	}{
		{name: "empty", raw: "", wantOK: false},
		{name: "spaces", raw: " \t ", wantOK: false},
		{name: "false", raw: "false", wantOK: false},
		{name: "no", raw: "NO", wantOK: false},
		{name: "true", raw: "true", wantAddr: "127.0.0.1:6060", wantOK: true},
		{name: "one", raw: "1", wantAddr: "127.0.0.1:6060", wantOK: true},
		{name: "port", raw: "7070", wantAddr: "127.0.0.1:7070", wantOK: true},
		{name: "address", raw: "127.0.0.1:9090", wantAddr: "127.0.0.1:9090", wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAddr, gotOK := AddrFromEnvValue(tt.raw)
			if gotOK != tt.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotAddr != tt.wantAddr {
				t.Fatalf("addr = %q, want %q", gotAddr, tt.wantAddr)
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
