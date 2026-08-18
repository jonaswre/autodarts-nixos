package dataset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCatalogLoadsOnceAndTracksNewSamplesForDashboard(t *testing.T) {
	output := t.TempDir()
	first := writeCatalogTestSample(t, output, "20260819T100000.000000000Z-world", time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC), 12)
	catalog, err := NewCatalog(output, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if inventory := catalog.Inventory(); inventory.SampleCount != 1 || inventory.Samples[0].Label.SampleID != first.SampleID {
		t.Fatalf("startup catalog did not load existing sample: %+v", inventory)
	}

	second := writeCatalogTestSample(t, output, "20260819T100100.000000000Z-world", time.Date(2026, 8, 19, 10, 1, 0, 0, time.UTC), 20)
	if err := catalog.AddPath(second, filepath.Join(output, second.SampleID)); err != nil {
		t.Fatal(err)
	}
	inventory, err := (Dashboard{OutputDir: output, Catalog: catalog}).Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if inventory.SampleCount != 2 || inventory.Samples[0].Label.SampleID != second.SampleID || inventory.UsedBytes <= 0 {
		t.Fatalf("dashboard did not observe incrementally added sample: %+v", inventory)
	}
}

func TestCatalogQuotaRemovesOldestSampleAndUpdatesInventory(t *testing.T) {
	output := t.TempDir()
	first := writeCatalogTestSample(t, output, "20260819T100000.000000000Z-world", time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC), 64)
	catalog, err := NewCatalog(output, 0)
	if err != nil {
		t.Fatal(err)
	}
	firstSize := catalog.Inventory().UsedBytes
	second := writeCatalogTestSample(t, output, "20260819T100100.000000000Z-world", time.Date(2026, 8, 19, 10, 1, 0, 0, time.UTC), 64)
	if err := catalog.AddPath(second, filepath.Join(output, second.SampleID)); err != nil {
		t.Fatal(err)
	}
	catalog.quota = catalog.Inventory().UsedBytes - firstSize
	if err := catalog.EnforceQuota(); err != nil {
		t.Fatal(err)
	}
	inventory := catalog.Inventory()
	if inventory.SampleCount != 1 || inventory.Samples[0].Label.SampleID != second.SampleID {
		t.Fatalf("quota did not retain only newest sample: %+v", inventory)
	}
	if _, err := os.Stat(filepath.Join(output, first.SampleID)); !os.IsNotExist(err) {
		t.Fatalf("oldest sample still exists: %v", err)
	}
}

func writeCatalogTestSample(t *testing.T, output, sampleID string, capturedAt time.Time, frameBytes int) Label {
	t.Helper()
	directory := filepath.Join(output, sampleID)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "cam-0.jpg"), make([]byte, frameBytes), 0600); err != nil {
		t.Fatal(err)
	}
	label := Label{
		SchemaVersion: SchemaVersion, SampleID: sampleID, CapturedAt: capturedAt, LabelSource: "autodarts",
		Supervision: "teacher-world-state", CaptureReason: "periodic-world-observation", ReviewStatus: "unreviewed",
		WorldAfter: &PhysicalBoardState{Confidence: WorldConfidenceTeacher},
	}
	data, err := json.Marshal(label)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "label.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return label
}
