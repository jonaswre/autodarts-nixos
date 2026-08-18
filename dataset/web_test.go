package dataset

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonaswre/autodarts-nixos/autodarts"
)

func TestDashboardShowsQuotaAndEmptyDataset(t *testing.T) {
	dashboard := Dashboard{OutputDir: filepath.Join(t.TempDir(), "not-created"), Quota: 20 << 30}
	page := httptest.NewRecorder()
	dashboard.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "auto<span>darts</span> dataset") || !strings.Contains(page.Body.String(), "/api/inventory") {
		t.Fatalf("dashboard page status=%d body=%s", page.Code, page.Body.String())
	}

	response := httptest.NewRecorder()
	dashboard.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/inventory", nil))
	var inventory Inventory
	if err := json.Unmarshal(response.Body.Bytes(), &inventory); err != nil {
		t.Fatal(err)
	}
	if inventory.SampleCount != 0 || inventory.QuotaBytes != 20<<30 || inventory.Samples == nil {
		t.Fatalf("unexpected empty inventory: %+v", inventory)
	}
}

func TestDashboardPreviewsRecordedSampleWithExactCoordinates(t *testing.T) {
	output := t.TempDir()
	sampleID := "20260813T184205.381000000Z-dart-2"
	directory := filepath.Join(output, sampleID)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	frames := FrameFiles{
		Before: []string{"cam-0-before.jpg", "cam-1-before.jpg", "cam-2-before.jpg"},
		After:  []string{"cam-0-after.jpg", "cam-1-after.jpg", "cam-2-after.jpg"},
	}
	for _, name := range append(frames.Before, frames.After...) {
		if err := os.WriteFile(filepath.Join(directory, name), []byte{0xff, 0xd8, 1, 2, 0xff, 0xd9}, 0600); err != nil {
			t.Fatal(err)
		}
	}
	label := Label{
		SchemaVersion: SchemaVersion, SampleID: sampleID, CapturedAt: time.Date(2026, 8, 13, 18, 42, 5, 381000000, time.UTC),
		LabelSource: "autodarts", Coordinates: autodarts.Coordinates{X: 0.1277737042977262, Y: 0.1502979414527876},
		Segment: autodarts.Segment{Name: "S18", Number: 18, Multiplier: 1, Bed: "SingleInner"}, DartIndex: 2, DartCount: 2, Frames: frames, ReviewStatus: "unreviewed",
	}
	data, _ := json.Marshal(label)
	if err := os.WriteFile(filepath.Join(directory, "label.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	dashboard := Dashboard{OutputDir: output, Quota: 1024}
	response := httptest.NewRecorder()
	dashboard.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/inventory", nil))
	var inventory Inventory
	if err := json.Unmarshal(response.Body.Bytes(), &inventory); err != nil {
		t.Fatal(err)
	}
	if inventory.SampleCount != 1 || inventory.Samples[0].Label.Coordinates != label.Coordinates || inventory.UsedBytes == 0 {
		t.Fatalf("unexpected populated inventory: %+v", inventory)
	}

	image := httptest.NewRecorder()
	dashboard.ServeHTTP(image, httptest.NewRequest(http.MethodGet, "/samples/"+sampleID+"/cam-0-before.jpg", nil))
	if image.Code != http.StatusOK || image.Header().Get("Content-Type") != "image/jpeg" || len(image.Body.Bytes()) != 6 {
		t.Fatalf("image status=%d type=%s bytes=%d", image.Code, image.Header().Get("Content-Type"), image.Body.Len())
	}

	traversal := httptest.NewRecorder()
	dashboard.ServeHTTP(traversal, httptest.NewRequest(http.MethodGet, "/samples/../label.json", nil))
	if traversal.Code != http.StatusNotFound {
		t.Fatalf("path traversal status=%d", traversal.Code)
	}
}
