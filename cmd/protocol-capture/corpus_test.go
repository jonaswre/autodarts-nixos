package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type eventEnvelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func TestCapturedCorpusIsValidAndRedacted(t *testing.T) {
	root := filepath.Join("..", "..", "protocol", "testdata")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, forbidden := range []string{"/dev/video", "usb-0000", `"api_key":"secret"`, "jonaswre", "Büro", "0d23d72f-c84f-4bfe-bd76-c20375c43c07"} {
			if contains(data, forbidden) {
				t.Errorf("%s contains private value %q", path, forbidden)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCapturedEventsHaveTypedJSONEnvelopes(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "protocol", "testdata", "captures", "*", "events.jsonl"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("event fixtures: %v, %v", paths, err)
	}
	seen := map[string]bool{}
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 4<<20)
		for scanner.Scan() {
			var event eventEnvelope
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				t.Errorf("%s: %v", path, err)
				continue
			}
			if event.Type == "" || len(event.Data) == 0 {
				t.Errorf("%s: incomplete envelope", path)
			}
			seen[event.Type] = true
		}
		file.Close()
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
	}
	for _, required := range []string{"state", "motion_state", "stats", "cam_state", "cam_stats"} {
		if !seen[required] {
			t.Errorf("missing real %s event", required)
		}
	}
}

func TestAutomaticCalibrationLifecycleWasCaptured(t *testing.T) {
	path := filepath.Join("..", "..", "protocol", "testdata", "captures", "board-1.0.7-calibration-session", "events.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{`"event":"Calibration started"`, `"event":"Calibration finished"`} {
		if !contains(data, event) {
			t.Errorf("missing %s", event)
		}
	}
	if contains(data, `"type":"calibration_state"`) {
		t.Error("automatic calibration unexpectedly emitted interactive calibration_state")
	}
}

func contains(data []byte, text string) bool {
	for index := 0; index+len(text) <= len(data); index++ {
		if string(data[index:index+len(text)]) == text {
			return true
		}
	}
	return false
}
