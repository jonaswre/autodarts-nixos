package dataset

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestAcceptedDartCreatesLabeledThreeCameraSample(t *testing.T) {
	var streams atomic.Int32
	streamReady := make(chan struct{})
	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"running": true, "status": "Throw", "event": "Started", "numThrows": 0})
	})
	mux.HandleFunc("/api/streams/cams/", func(w http.ResponseWriter, r *http.Request) {
		camera := r.URL.Path[len("/api/streams/cams/"):]
		writer := multipart.NewWriter(w)
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+writer.Boundary())
		w.WriteHeader(http.StatusOK)
		if streams.Add(1) == 3 {
			close(streamReady)
		}
		for sequence := byte(1); ; sequence++ {
			part, err := writer.CreatePart(textprotoHeader("image/jpeg"))
			if err != nil {
				return
			}
			if _, err := part.Write([]byte{0xff, 0xd8, byte(camera[0]), sequence, 0xff, 0xd9}); err != nil {
				return
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			select {
			case <-r.Context().Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		select {
		case <-streamReady:
		case <-r.Context().Done():
			return
		}
		time.Sleep(150 * time.Millisecond)
		event := map[string]any{
			"type": "state",
			"data": map[string]any{
				"running": true, "status": "Throw", "event": "Throw detected", "numThrows": 1,
				"throws": []any{map[string]any{
					"coords":  map[string]any{"x": 0.1277737042977262, "y": 0.1502979414527876},
					"segment": map[string]any{"name": "S18", "number": 18, "multiplier": 1, "bed": "SingleInner"},
				}},
			},
		}
		if err := connection.WriteJSON(event); err != nil {
			t.Error(err)
			return
		}
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	output := t.TempDir()
	recorder, err := New(Options{BoardURL: server.URL, OutputDir: output, PreDelay: 80 * time.Millisecond, PostDelay: 80 * time.Millisecond, Quota: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- recorder.Run(ctx) }()

	var sampleDir string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := os.ReadDir(output)
		for _, entry := range entries {
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				sampleDir = filepath.Join(output, entry.Name())
				break
			}
		}
		if sampleDir != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if sampleDir == "" {
		t.Fatal("accepted dart did not create a sample")
	}

	data, err := os.ReadFile(filepath.Join(sampleDir, "label.json"))
	if err != nil {
		t.Fatal(err)
	}
	var label Label
	if err := json.Unmarshal(data, &label); err != nil {
		t.Fatal(err)
	}
	if label.Coordinates.X != 0.1277737042977262 || label.Coordinates.Y != 0.1502979414527876 {
		t.Fatalf("coordinates lost precision: %+v", label.Coordinates)
	}
	if label.Segment.Name != "S18" || label.DartIndex != 1 || label.LabelSource != "autodarts" {
		t.Fatalf("unexpected label: %+v", label)
	}
	if len(label.Frames.Before) != 3 || len(label.Frames.After) != 3 {
		t.Fatalf("frame manifest: %+v", label.Frames)
	}
	for _, name := range append(label.Frames.Before, label.Frames.After...) {
		image, err := os.ReadFile(filepath.Join(sampleDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if len(image) < 4 || image[0] != 0xff || image[1] != 0xd8 {
			t.Fatalf("%s is not a JPEG", name)
		}
	}
	for camera := range label.Frames.Before {
		before, _ := os.ReadFile(filepath.Join(sampleDir, label.Frames.Before[camera]))
		after, _ := os.ReadFile(filepath.Join(sampleDir, label.Frames.After[camera]))
		if bytes.Equal(before, after) {
			t.Fatalf("camera %d before/after frames were not synchronized to different times", camera)
		}
	}
}

func TestQuotaRemovesOldestCompleteSamples(t *testing.T) {
	output := t.TempDir()
	for _, name := range []string{"20260101T000000.000000000Z-dart-1", "20260102T000000.000000000Z-dart-1"} {
		if err := os.Mkdir(filepath.Join(output, name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, name, "frame.jpg"), make([]byte, 10), 0600); err != nil {
			t.Fatal(err)
		}
	}
	recorder := &Recorder{options: Options{OutputDir: output, Quota: 10}}
	if err := recorder.enforceQuota(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(output, "20260101T000000.000000000Z-dart-1")); !os.IsNotExist(err) {
		t.Fatal("oldest sample was not removed")
	}
	if _, err := os.Stat(filepath.Join(output, "20260102T000000.000000000Z-dart-1")); err != nil {
		t.Fatal("newest sample was removed")
	}
}

func textprotoHeader(contentType string) textproto.MIMEHeader {
	return textproto.MIMEHeader{"Content-Type": []string{contentType}}
}
