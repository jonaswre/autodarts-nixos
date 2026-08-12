package main

import (
	"crypto/tls"
	"github.com/gorilla/websocket"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveBridgeReachesWayVNC(t *testing.T) {
	endpoint := os.Getenv("AUTODARTS_NOVNC_TEST_URL")
	if endpoint == "" {
		t.Skip("set AUTODARTS_NOVNC_TEST_URL for live appliance test")
	}
	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, Subprotocols: []string{"binary"}, HandshakeTimeout: 5 * time.Second}
	socket, _, err := dialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	socket.SetReadDeadline(time.Now().Add(5 * time.Second))
	kind, data, err := socket.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if kind != websocket.BinaryMessage || !strings.HasPrefix(string(data), "RFB ") {
		t.Fatalf("expected RFB greeting, kind=%d data=%q", kind, data)
	}
}

func TestServesNoVNCAndProxiesBinaryVNC(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "vnc.html"), []byte("noVNC_credentials"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()
	server := httptest.NewTLSServer(newBridge(root, listener.Addr().String()))
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/vnc.html")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if !strings.Contains(string(page), "noVNC_credentials") {
		t.Fatalf("page=%s", page)
	}
	endpoint := "wss" + strings.TrimPrefix(server.URL, "https") + "/websockify"
	dialer := websocket.Dialer{TLSClientConfig: server.Client().Transport.(*http.Transport).TLSClientConfig, Subprotocols: []string{"binary"}}
	socket, _, err := dialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	if err := socket.WriteMessage(websocket.BinaryMessage, []byte("RFB 003.008\n")); err != nil {
		t.Fatal(err)
	}
	kind, data, err := socket.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if kind != websocket.BinaryMessage || string(data) != "RFB 003.008\n" {
		t.Fatalf("kind=%d data=%q", kind, data)
	}
}
