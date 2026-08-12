package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
)

func testService(t *testing.T) *service {
	t.Helper()
	return &service{stateDir: t.TempDir(), assetDir: "../../onboarding", calibration: "idle", client: http.DefaultClient}
}

func TestPlaySanitizerPreservesDartButRemovesIdentity(t *testing.T) {
	s := testService(t)
	id := "4dff4c92-3451-450e-9cc4-d163090a156a"
	input := map[string]any{"id": id, "topic": id + ".state", "board": "Private Board", "boardName": "Private Board", "player": "Secret Player", "access_token": "secret", "boards": map[string]any{id: map[string]any{"score": 64}}, "segment": map[string]any{"name": "T20", "number": 20, "bed": "Triple", "multiplier": 3}}
	encoded, err := json.Marshal(s.sanitize(input))
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, secret := range []string{id, "Private Board", "Secret Player", `"secret"`} {
		if strings.Contains(text, secret) {
			t.Errorf("sanitized event contains %q", secret)
		}
	}
	if !strings.Contains(text, `"name":"T20"`) {
		t.Fatalf("dart segment was damaged: %s", text)
	}
}

func TestPairingDetailsRejectRemoteClients(t *testing.T) {
	s := testService(t)
	request := httptest.NewRequest(http.MethodGet, "http://appliance/api/pairing", nil)
	request.RemoteAddr = "192.0.2.44:1234"
	response := httptest.NewRecorder()
	s.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", response.Code)
	}
	if !strings.Contains(response.Body.String(), "only shown on the appliance display") {
		t.Fatalf("response=%s", response.Body.String())
	}
}

func TestPlayCaptureJourneyWritesPrivateState(t *testing.T) {
	s := testService(t)
	record := func(path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "http://appliance"+path, strings.NewReader(body))
		request.RemoteAddr = "127.0.0.1:1234"
		response := httptest.NewRecorder()
		s.ServeHTTP(response, request)
		return response
	}
	if response := record("/api/play-capture/start", `{}`); response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	payload := `{"url":"wss://play.ws.autodarts.io/ms/v0/subscribe","data":"{\"segment\":{\"name\":\"D2\",\"number\":2,\"bed\":\"Double\",\"multiplier\":2}}","transport":"websocket"}`
	if response := record("/api/play-event", payload); response.Code != 204 {
		t.Fatal(response.Body.String())
	}
	data, err := os.ReadFile(s.file("play-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"name":"D2"`) {
		t.Fatalf("state=%s", data)
	}
	info, err := os.Stat(s.file("play-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o, want 600", info.Mode().Perm())
	}
}
