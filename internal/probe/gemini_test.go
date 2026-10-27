package probe

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// socksToLocal starts a minimal SOCKS5 server that ignores the requested
// target and always connects to backend, so the HTTP client can be pointed at
// a local httptest server through the real dialSOCKS5 path.
func socksToLocal(t *testing.T, backend string) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 512)
				c.Read(buf)
				c.Write([]byte{5, 0})
				c.Read(buf)
				up, err := net.Dial("tcp", backend)
				if err != nil {
					c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
					return
				}
				defer up.Close()
				c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
				done := make(chan struct{}, 2)
				go func() { copyAll(up, c); done <- struct{}{} }()
				go func() { copyAll(c, up); done <- struct{}{} }()
				<-done
			}(c)
		}
	}()
	return l.Addr().String()
}

func copyAll(dst, src net.Conn) {
	buf := make([]byte, 4096)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			dst.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func TestGeminiVerdicts(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		status  string
	}{
		{"ok", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }, "ok"},
		{"429", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) }, "blocked"},
		{"sorry redirect", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "/sorry/index?continue=x")
			w.WriteHeader(302)
		}, "blocked"},
		{"consent redirect then ok", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/app" {
				w.Header().Set("Location", "/consent")
				w.WriteHeader(302)
				return
			}
			w.WriteHeader(200)
		}, "ok"},
		{"500", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }, "error"},
		{"redirect loop", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "/app")
			w.WriteHeader(302)
		}, "error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := httptest.NewServer(tc.handler)
			defer backend.Close()
			socks := socksToLocal(t, backend.Listener.Addr().String())
			got := geminiAt(context.Background(), socks, "http://gemini.test/app", 5*time.Second)
			if got.Status != tc.status {
				t.Fatalf("status = %q (%+v), want %q", got.Status, got, tc.status)
			}
		})
	}
}
