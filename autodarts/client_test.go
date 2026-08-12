package autodarts

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestUserCanInspectAndControlBoard(t *testing.T) {
	fixtures := filepath.Join("..", "protocol", "testdata", "captures", "board-1.0.7-three-darts", "http")
	var mu sync.Mutex
	var actions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			mu.Lock()
			actions = append(actions, r.Method+" "+r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		name := r.URL.Path[len("/api/"):]
		name = replaceSlash(name) + ".json"
		data, err := os.ReadFile(filepath.Join(fixtures, name))
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
	defer server.Close()
	client, err := New(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	state, err := client.BoardState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "Takeout" || len(state.Throws) != 3 || state.Throws[1].Segment.Name != "S18" {
		t.Fatalf("unexpected visible board state: %+v", state)
	}
	if err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.ResetVisit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.AutoCalibrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	controls := []func(context.Context) error{
		client.StartStreaming, client.StopStreaming, client.ConnectUpstream, client.DisconnectUpstream,
		client.RevertConfig, client.ResetAuthentication,
		func(ctx context.Context) error { return client.SetCalibration(ctx, map[string]any{}) },
		func(ctx context.Context) error { return client.SetDistortion(ctx, map[string]any{}) },
		func(ctx context.Context) error { return client.SetDistortionEnabled(ctx, 1, true) },
		func(ctx context.Context) error { return client.SetDistortionAlpha(ctx, 1, 0.5) },
		func(ctx context.Context) error { return client.RotateCalibrationLeft(ctx, 1) },
		func(ctx context.Context) error {
			return client.PatchCameraControls(ctx, 1, map[string]any{"exposure": 10})
		},
		func(ctx context.Context) error { return client.ResetCameraControls(ctx, 1) },
	}
	for _, control := range controls {
		if err := control(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		"PUT /api/start", "POST /api/reset", "POST /api/config/calibration/auto", "PUT /api/stop",
		"PUT /api/streams/start", "PUT /api/streams/stop", "PUT /api/upstream/connect", "PUT /api/upstream/disconnect",
		"POST /api/config/revert", "POST /api/config/auth/reset", "PUT /api/config/calibration", "PUT /api/config/distortion",
		"PUT /api/config/distortion/1/enabled", "PUT /api/config/distortion/1/alpha", "POST /api/config/calibration/auto/1/rotate-left",
		"PATCH /api/cams/controls/1", "POST /api/cams/controls/1/reset",
	}
	if !reflect.DeepEqual(actions, want) {
		t.Fatalf("actions = %v, want %v", actions, want)
	}
}

func TestUserReceivesLiveBoardState(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.WriteJSON(Event{Type: "state", Data: json.RawMessage(`{"connected":true,"status":"Throw","event":"Throw detected","numThrows":1,"throws":[{"coords":{"x":0.1,"y":0.2},"segment":{"name":"T20","number":20,"bed":"Triple","multiplier":3}}]}`)})
		<-r.Context().Done()
	}))
	defer server.Close()
	client, _ := New(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	events, failures, err := client.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if changed, err := client.ApplyBoardEvent(event); err != nil || !changed {
			t.Fatalf("apply: %v, %v", changed, err)
		}
		state := client.Snapshot().Board
		if state.Event != "Throw detected" || len(state.Throws) != 1 || state.Throws[0].Segment.Name != "T20" {
			t.Fatalf("visible live state: %+v", state)
		}
	case err := <-failures:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal("timed out waiting for live board state")
	}
}

func TestUnifiedJourneyBoardThrowToMatchCheckout(t *testing.T) {
	client, _ := New("http://board.invalid:3180")
	boardPath := filepath.Join("..", "protocol", "testdata", "captures", "board-1.0.7-takeout-session", "events.jsonl")
	file, err := os.Open(boardPath)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	seenTakeout := false
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		changed, err := client.ApplyBoardEvent(event)
		if err != nil {
			t.Fatal(err)
		}
		if changed && client.Snapshot().Board.Status == "Takeout in progress" {
			seenTakeout = true
		}
	}
	file.Close()
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !seenTakeout {
		t.Fatal("real board journey never reached takeout")
	}

	playPath := filepath.Join("..", "protocol", "testdata", "play", "random-checkout-64.jsonl")
	file, err = os.Open(playPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner = bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	firstTarget := ""
	for scanner.Scan() {
		changed, err := client.ApplyPlayEvent(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if changed && firstTarget == "" {
			if target, ok := client.Snapshot().NextTarget(); ok {
				firstTarget = target.Name
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	snapshot := client.Snapshot()
	if firstTarget != "T16" {
		t.Fatalf("initial target = %s", firstTarget)
	}
	if !snapshot.MatchFinished || snapshot.Remaining != 0 || snapshot.LastThrow == nil || snapshot.LastThrow.Name != "D2" {
		t.Fatalf("unexpected final unified state: %+v", snapshot)
	}
	if _, ok := snapshot.NextTarget(); ok {
		t.Fatal("finished match exposes target")
	}
}

func replaceSlash(value string) string {
	result := []byte(value)
	for i := range result {
		if result[i] == '/' {
			result[i] = '-'
		}
	}
	return string(result)
}
