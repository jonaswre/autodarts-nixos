package main

import (
	"context"
	"flag"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type bridge struct {
	webRoot, target string
	dialer          net.Dialer
}

var upgrader = websocket.Upgrader{ReadBufferSize: 32 << 10, WriteBufferSize: 32 << 10, Subprotocols: []string{"binary"}, CheckOrigin: func(*http.Request) bool { return true }}

func main() {
	listen := flag.String("listen", "0.0.0.0:6080", "HTTPS listen address")
	target := flag.String("target", "127.0.0.1:5900", "VNC TCP target")
	web := flag.String("web", "", "noVNC web root")
	cert := flag.String("cert", "", "TLS certificate")
	key := flag.String("key", "", "TLS private key")
	flag.Parse()
	if *web == "" || *cert == "" || *key == "" {
		flag.Usage()
		os.Exit(2)
	}
	server := &http.Server{Addr: *listen, Handler: newBridge(*web, *target), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second}
	log.Fatal(server.ListenAndServeTLS(*cert, *key))
}
func newBridge(webRoot, target string) http.Handler {
	return &bridge{webRoot: webRoot, target: target, dialer: net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}}
}
func (b *bridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/websockify" || r.URL.Path == "/websockify/" {
		b.proxy(w, r)
		return
	}
	path := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if path == "." {
		path = "vnc.html"
	}
	full := filepath.Join(b.webRoot, path)
	root, err := filepath.Abs(b.webRoot)
	if err != nil {
		http.Error(w, "invalid web root", 500)
		return
	}
	resolved, err := filepath.Abs(full)
	if err != nil || !(resolved == root || strings.HasPrefix(resolved, root+string(filepath.Separator))) {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(resolved)
	if err == nil && info.IsDir() {
		resolved = filepath.Join(resolved, "index.html")
	}
	if ext := filepath.Ext(resolved); ext != "" {
		if kind := mime.TypeByExtension(ext); kind != "" {
			w.Header().Set("Content-Type", kind)
		}
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, resolved)
}
func (b *bridge) proxy(w http.ResponseWriter, r *http.Request) {
	tcp, err := b.dialer.DialContext(r.Context(), "tcp", b.target)
	if err != nil {
		http.Error(w, "VNC target unavailable", http.StatusBadGateway)
		return
	}
	defer tcp.Close()
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	fail := make(chan error, 2)
	go func() {
		for {
			kind, data, err := ws.ReadMessage()
			if err != nil {
				fail <- err
				return
			}
			if kind != websocket.BinaryMessage && kind != websocket.TextMessage {
				continue
			}
			if _, err = tcp.Write(data); err != nil {
				fail <- err
				return
			}
		}
	}()
	go func() {
		buffer := make([]byte, 32<<10)
		for {
			count, err := tcp.Read(buffer)
			if count > 0 {
				if writeErr := ws.WriteMessage(websocket.BinaryMessage, buffer[:count]); writeErr != nil {
					fail <- writeErr
					return
				}
			}
			if err != nil {
				fail <- err
				return
			}
		}
	}()
	select {
	case <-ctx.Done():
	case <-fail:
	}
}
