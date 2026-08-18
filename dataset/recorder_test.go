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
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jonaswre/autodarts-nixos/autodarts"
)

func drainErrors(values <-chan error) []error {
	var result []error
	for {
		select {
		case value := <-values:
			result = append(result, value)
		default:
			return result
		}
	}
}

func TestRecorderCapturesOnlyAutodartsTeacherWorldJourney(t *testing.T) {
	var streams atomic.Int32
	var stateRequests atomic.Int32
	streamReady := make(chan struct{})
	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, _ *http.Request) {
		status, event := "Throw", "Started"
		if stateRequests.Add(1) <= 2 {
			status, event = "Takeout in progress", "Takeout started"
		}
		json.NewEncoder(w).Encode(map[string]any{"connected": true, "running": true, "status": status, "event": event, "numThrows": 0})
	})
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode("1.0.7")
	})
	mux.HandleFunc("/api/host", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"clientVersion": "1.0.7", "hostname": "must-not-leak", "ip": "192.0.2.42",
			"os": "linux", "platform": "nixos", "platformVersion": "26.05", "kernelArch": "x86_64", "kernelVersion": "6.18",
		})
	})
	mux.HandleFunc("/api/cams/state", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"isOpened": true, "isRunning": true})
	})
	mux.HandleFunc("/api/cams/stats", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"fps": []float64{30, 30, 30}, "resolution": map[string]int{"width": 1280, "height": 720}})
	})
	mux.HandleFunc("/api/state/stats", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"fps": 30, "resolution": map[string]int{"width": 1280, "height": 720}})
	})
	mux.HandleFunc("/api/devices", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{{"bus": "usb-1", "card": "camera-a", "vendorId": 123, "productId": 456}})
	})
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"auth": map[string]any{"api_key": "DO-NOT-STORE"}, "board_id": "DO-NOT-STORE",
			"board_rois": []any{map[string]any{"x": 1, "y": 2}},
			"cam":        map[string]any{"resolution": map[string]int{"width": 1280, "height": 720}, "cams": []string{"/dev/video0"}},
			"dartboard":  map[string]any{"center": map[string]float64{"x": 0.5, "y": 0.5}},
			"detection":  map[string]any{"threshold": 42},
			"motion":     map[string]any{"threshold": 7},
		})
	})
	mux.HandleFunc("/api/config/calibration", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"points": []int{1, 2, 3}, "token": "DO-NOT-STORE"})
	})
	mux.HandleFunc("/api/config/distortion", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"coefficients": []float64{0.1, -0.2}})
	})
	mux.HandleFunc("/api/config/calibration/ellipses", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{{"center": []float64{0.5, 0.5}}})
	})
	mux.HandleFunc("/api/config/cam/resolution", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]int{"width": 1280, "height": 720})
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
		// Let the recorder establish its stable empty-board baseline before the
		// user begins the first throw journey.
		time.Sleep(1500 * time.Millisecond)
		if err := connection.WriteJSON(map[string]any{
			"type": "motion_state", "data": map[string]any{
				"isStable": false, "frameFlags": map[string]bool{"dart": true},
				"camStates": []map[string]any{{"isStable": false, "isDart": true}},
			},
		}); err != nil {
			t.Error(err)
			return
		}
		time.Sleep(50 * time.Millisecond)
		if err := connection.WriteJSON(map[string]any{
			"type": "motion_state", "data": map[string]any{"isStable": true, "frameFlags": map[string]bool{}},
		}); err != nil {
			t.Error(err)
			return
		}
		time.Sleep(50 * time.Millisecond)
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
		time.Sleep(100 * time.Millisecond)
		if err := connection.WriteJSON(map[string]any{
			"type": "motion_state", "data": map[string]any{
				"isStable": false, "isTakeoutPartial": true,
				"frameFlags": map[string]bool{"takeout": true},
			},
		}); err != nil {
			t.Error(err)
			return
		}
		time.Sleep(50 * time.Millisecond)
		if err := connection.WriteJSON(map[string]any{
			"type": "motion_state", "data": map[string]any{"isStable": true, "frameFlags": map[string]bool{}},
		}); err != nil {
			t.Error(err)
			return
		}
		time.Sleep(100 * time.Millisecond)
		if err := connection.WriteJSON(map[string]any{
			"type": "motion_state", "data": map[string]any{
				"isStable": false, "frameFlags": map[string]bool{"dart": true},
			},
		}); err != nil {
			t.Error(err)
			return
		}
		time.Sleep(50 * time.Millisecond)
		if err := connection.WriteJSON(map[string]any{
			"type": "motion_state", "data": map[string]any{"isStable": true, "frameFlags": map[string]bool{}},
		}); err != nil {
			t.Error(err)
			return
		}
		time.Sleep(50 * time.Millisecond)
		if err := connection.WriteJSON(map[string]any{
			"type": "state", "data": map[string]any{
				"running": true, "status": "Throw", "event": "Throw detected", "numThrows": 2,
				"throws": []any{
					map[string]any{
						"coords":  map[string]any{"x": 0.1277737042977262, "y": 0.1502979414527876},
						"segment": map[string]any{"name": "S18", "number": 18, "multiplier": 1, "bed": "SingleInner"},
					},
					map[string]any{
						"coords":  map[string]any{"x": -0.225, "y": 0.375},
						"segment": map[string]any{"name": "T19", "number": 19, "multiplier": 3, "bed": "Triple"},
					},
				},
			},
		}); err != nil {
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
	captureErrors := make(chan error, 16)
	recorder, err := New(Options{
		BoardURL: server.URL, OutputDir: output, PreDelay: 80 * time.Millisecond, PostDelay: 80 * time.Millisecond,
		BurstOffsets: []time.Duration{-80 * time.Millisecond, 0, 80 * time.Millisecond}, Quota: 1 << 20,
		OnError: func(err error) { captureErrors <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- recorder.Run(ctx) }()

	type recordedSample struct {
		label Label
		path  string
	}
	var samples []recordedSample
	seen := map[string]bool{}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := os.ReadDir(output)
		for _, entry := range entries {
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				directory := filepath.Join(output, entry.Name())
				data, readErr := os.ReadFile(filepath.Join(directory, "label.json"))
				var value Label
				if readErr == nil && json.Unmarshal(data, &value) == nil && !seen[value.SampleID] {
					seen[value.SampleID] = true
					samples = append(samples, recordedSample{label: value, path: directory})
				}
			}
		}
		counts := map[string]int{}
		for _, sample := range samples {
			counts[sample.label.CaptureReason]++
		}
		if counts["startup-world-observation"] == 1 && counts["teacher-dart-added"] >= 2 {
			time.Sleep(150 * time.Millisecond)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var startup Label
	var accepted []recordedSample
	for _, sample := range samples {
		if sample.label.CaptureReason == "motion-started" || sample.label.CaptureReason == "motion-settled" || sample.label.ReviewStatus == "required" || sample.label.Supervision == "unlabeled-world-transition" {
			t.Fatalf("motion created a standalone training sample: %+v", sample.label)
		}
		switch sample.label.CaptureReason {
		case "startup-world-observation":
			startup = sample.label
		case "teacher-dart-added":
			accepted = append(accepted, sample)
		}
	}
	sort.Slice(accepted, func(i, j int) bool { return accepted[i].label.CapturedAt.Before(accepted[j].label.CapturedAt) })
	if startup.SampleID == "" || startup.WorldAfter == nil || len(startup.WorldAfter.Darts) != 0 || startup.Context.BoardState.Status != "Throw" || stateRequests.Load() < 3 {
		t.Fatalf("startup did not record an empty physical world: %+v errors=%v", startup, drainErrors(captureErrors))
	}
	startupHashes := map[int]map[string]bool{}
	for _, frame := range startup.FrameSequence {
		if frame.Role != "stable-world" || frame.WorldState != "after" {
			t.Fatalf("startup frame is missing its stable-world semantics: %+v", frame)
		}
		if startupHashes[frame.Camera] == nil {
			startupHashes[frame.Camera] = map[string]bool{}
		}
		startupHashes[frame.Camera][frame.SHA256] = true
	}
	for camera := 0; camera < 3; camera++ {
		if len(startupHashes[camera]) < 2 {
			t.Fatalf("camera %d startup pre-roll was duplicated instead of primed: %+v", camera, startup.FrameSequence)
		}
	}
	if len(accepted) != 2 {
		t.Fatalf("teacher journey samples missing: accepted=%d all=%+v errors=%v", len(accepted), samples, drainErrors(captureErrors))
	}
	label := accepted[0].label
	sampleDir := accepted[0].path
	if label.Coordinates.X != 0.1277737042977262 || label.Coordinates.Y != 0.1502979414527876 {
		t.Fatalf("coordinates lost precision: %+v", label.Coordinates)
	}
	if label.SchemaVersion != 3 || !label.HasCoordinates || label.Segment.Name != "S18" || label.DartIndex != 1 || label.LabelSource != "autodarts" {
		t.Fatalf("unexpected label: %+v", label)
	}
	if label.WorldBefore == nil || len(label.WorldBefore.Darts) != 0 || label.WorldAfter == nil || len(label.WorldAfter.Darts) != 1 || label.Transition == nil || len(label.Transition.Added) != 1 {
		t.Fatalf("first dart did not describe the complete physical world transition: %+v", label)
	}
	if label.Transition.Added[0].Coordinates != label.Coordinates || label.WorldAfter.Darts[0].ID == "" {
		t.Fatalf("first dart transition lost its exact label or stable identity: %+v", label.Transition)
	}
	second := accepted[1].label
	if second.Coordinates != (autodarts.Coordinates{X: -0.225, Y: 0.375}) || second.WorldBefore == nil || len(second.WorldBefore.Darts) != 1 || second.WorldAfter == nil || len(second.WorldAfter.Darts) != 2 {
		t.Fatalf("second dart world state is incomplete: %+v", second)
	}
	if second.ReviewStatus != "unreviewed" || second.WorldAfter.Confidence != WorldConfidenceTeacher || second.Transition == nil || second.Transition.Confidence != WorldConfidenceTeacher || len(second.Transition.Added) != 1 || len(second.Transition.StillPresent) != 1 || len(second.Transition.Uncertain) != 0 {
		t.Fatalf("Autodarts teacher state was not retained as trusted truth: %+v", second)
	}
	if second.WorldBefore.Darts[0].ID != second.WorldAfter.Darts[0].ID {
		t.Fatalf("persistent dart identity changed across observations: before=%+v after=%+v", second.WorldBefore, second.WorldAfter)
	}
	if label.SessionID == "" || label.SetupID == "" || label.SetupFile == "" {
		t.Fatalf("sample is missing setup identity: %+v", label)
	}
	if len(label.FrameSequence) != 9 {
		t.Fatalf("captured %d frames, want 3 offsets from 3 cameras", len(label.FrameSequence))
	}
	for _, frame := range label.FrameSequence {
		if frame.File == "" || frame.CapturedAt.IsZero() || len(frame.SHA256) != 64 || frame.Role == "" {
			t.Fatalf("incomplete frame metadata: %+v", frame)
		}
		switch frame.RequestedOffsetMS {
		case -80:
			if frame.Role != "stable-before" || frame.WorldState != "before" {
				t.Fatalf("before frame role: %+v", frame)
			}
		case 0:
			if frame.Role != "transition" || frame.WorldState != "" {
				t.Fatalf("transition frame role: %+v", frame)
			}
		case 80:
			if frame.Role != "stable-after" || frame.WorldState != "after" {
				t.Fatalf("after frame role: %+v", frame)
			}
		}
	}
	if _, ok := label.Context.LatestEvents["state"]; !ok || label.Context.LatestEvents["motion_state"].Type != "motion_state" || len(label.Context.RecentEvents) == 0 {
		t.Fatalf("event context was not retained: %+v", label.Context)
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
	setupData, err := os.ReadFile(filepath.Join(output, filepath.FromSlash(label.SetupFile)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(setupData), "DO-NOT-STORE") || strings.Contains(string(setupData), "must-not-leak") || strings.Contains(string(setupData), "/dev/video0") {
		t.Fatalf("setup snapshot leaked private configuration: %s", setupData)
	}
	var setup SetupSnapshot
	if err := json.Unmarshal(setupData, &setup); err != nil {
		t.Fatal(err)
	}
	if setup.SetupID != label.SetupID || setup.CameraResolution.Width != 1280 || len(setup.Calibration) == 0 || len(setup.DeviceFingerprints) != 1 {
		t.Fatalf("incomplete setup snapshot: %+v", setup)
	}
}

func TestRecorderRejectsNegativeWorldReferenceInterval(t *testing.T) {
	_, err := New(Options{BoardURL: "http://127.0.0.1:3180", OutputDir: t.TempDir(), ReferenceInterval: -time.Second})
	if err == nil || !strings.Contains(err.Error(), "reference interval") {
		t.Fatalf("negative interval error = %v", err)
	}
}

func TestStableWorldCaptureIsRejectedWhenMotionCrossesItsFrameWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/state" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"connected": true, "running": true, "status": "Throw", "numThrows": 0})
	}))
	defer server.Close()
	recorder, err := New(Options{BoardURL: server.URL, OutputDir: t.TempDir(), BurstOffsets: []time.Duration{-750 * time.Millisecond, 750 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	capturedAt := time.Now().UTC()
	recorder.rememberEvent(capturedAt, autodarts.Event{Type: "motion_state", Data: json.RawMessage(`{"isStable":false,"frameFlags":{"hand":true}}`)})
	recorder.rememberEvent(capturedAt.Add(100*time.Millisecond), autodarts.Event{Type: "motion_state", Data: json.RawMessage(`{"isStable":true}`)})
	world := PhysicalBoardState{Confidence: WorldConfidenceTeacher, Darts: []PhysicalDart{}}
	allowed, err := recorder.stableCaptureAllowed(context.Background(), Label{WorldAfter: &world}, capturedAt)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("motion inside the temporal burst was accepted as a stable-world observation")
	}
	allowed, err = recorder.stableCaptureAllowed(context.Background(), Label{WorldAfter: &world}, capturedAt.Add(2*time.Second))
	if err != nil || !allowed {
		t.Fatalf("settled board outside the motion window was rejected: allowed=%v err=%v", allowed, err)
	}
}

func TestStableBoardStateRequiresRunningThrowState(t *testing.T) {
	if !stableBoardState(autodarts.BoardState{Connected: true, Running: true, Status: "Throw"}) {
		t.Fatal("running Throw state was not considered stable")
	}
	for _, state := range []autodarts.BoardState{
		{Connected: true, Running: true, Status: "Takeout in progress"},
		{Connected: true, Running: false, Status: "Stopped"},
		{Connected: false, Running: true, Status: "Throw"},
	} {
		if stableBoardState(state) {
			t.Fatalf("transitional board state was considered stable: %+v", state)
		}
	}
}

func TestStartupStabilityRestartsWhenMotionCrossesTheObservationWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"connected": true, "running": true, "status": "Throw", "numThrows": 0})
	}))
	defer server.Close()
	recorder, err := New(Options{BoardURL: server.URL, OutputDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan autodarts.Event, 2)
	go func() {
		time.Sleep(250 * time.Millisecond)
		events <- autodarts.Event{Type: "motion_state", Data: json.RawMessage(`{"isStable":false,"frameFlags":{"hand":true}}`)}
		time.Sleep(100 * time.Millisecond)
		events <- autodarts.Event{Type: "motion_state", Data: json.RawMessage(`{"isStable":true}`)}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := time.Now()
	state, err := recorder.waitForStableBoardState(ctx, events, nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "Throw" || time.Since(started) < 750*time.Millisecond {
		t.Fatalf("startup observation did not wait for a fresh stable period after motion: state=%+v elapsed=%s", state, time.Since(started))
	}
}

func TestCameraHistoryRejectsARepeatedStaleFrame(t *testing.T) {
	now := time.Now().UTC()
	buffer := cameraBuffer{}
	buffer.add(frame{at: now.Add(-time.Second), jpeg: []byte{0xff, 0xd8}})
	if buffer.covers(now.Add(-750*time.Millisecond), now.Add(-maxFrameSkew)) {
		t.Fatal("one stale camera frame was treated as complete temporal history")
	}
	buffer.add(frame{at: now.Add(-900 * time.Millisecond), jpeg: []byte{0xff, 0xd8, 1}})
	if buffer.covers(now.Add(-750*time.Millisecond), now.Add(-maxFrameSkew)) {
		t.Fatal("history without a recent frame was accepted")
	}
	shortHistory := cameraBuffer{}
	shortHistory.add(frame{at: now.Add(-500 * time.Millisecond), jpeg: []byte{0xff, 0xd8, 2}})
	shortHistory.add(frame{at: now, jpeg: []byte{0xff, 0xd8, 3}})
	if shortHistory.covers(now.Add(-750*time.Millisecond), now.Add(-maxFrameSkew)) {
		t.Fatal("history without a frame at the requested start was accepted")
	}
	buffer.add(frame{at: now, jpeg: []byte{0xff, 0xd8, 4}})
	if !buffer.covers(now.Add(-750*time.Millisecond), now.Add(-maxFrameSkew)) {
		t.Fatal("real historical and current frames did not satisfy the capture window")
	}
}

func TestCaptureRejectsFreshTimestampsWithRepeatedImageBytes(t *testing.T) {
	output := t.TempDir()
	recorder := &Recorder{
		options: Options{
			OutputDir: output, Cameras: 1, PreDelay: 50 * time.Millisecond, PostDelay: 50 * time.Millisecond,
			BurstOffsets: []time.Duration{-50 * time.Millisecond, 0, 50 * time.Millisecond}, Quota: 1 << 20,
		},
		buffers:   make([]cameraBuffer, 1),
		sessionID: "session-test",
	}
	capturedAt := time.Now().UTC()
	for _, offset := range recorder.options.BurstOffsets {
		recorder.buffers[0].add(frame{at: capturedAt.Add(offset), jpeg: []byte{0xff, 0xd8, 1, 0xff, 0xd9}})
	}
	err := recorder.capture(context.Background(), capturedAt, "repeated", Label{CaptureReason: "teacher-dart-added"})
	if err == nil || !strings.Contains(err.Error(), "only one unique frame") {
		t.Fatalf("repeated-image capture error = %v", err)
	}
	entries, readErr := os.ReadDir(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			t.Fatalf("invalid repeated-image sample was committed: %s", entry.Name())
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
