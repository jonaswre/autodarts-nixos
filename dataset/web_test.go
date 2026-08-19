package dataset

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
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
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "auto<span>darts</span> dataset") || !strings.Contains(page.Body.String(), "/api/inventory?limit=24") || !strings.Contains(page.Body.String(), "/api/export/webdataset.tar") || !strings.Contains(page.Body.String(), "Excluded evidence") {
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

func TestDashboardOffersDetectedBoardFeatureOverlay(t *testing.T) {
	dashboard := Dashboard{OutputDir: t.TempDir(), Quota: 20 << 30}
	page := httptest.NewRecorder()
	dashboard.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	body := page.Body.String()
	for _, visibleBehavior := range []string{
		"Detected board features",
		"featuresToggle",
		"Detected dartboard features",
		"doubleOuters",
		"trebleOuters",
		"board_rois",
		"config?.calibration",
		"class=\"sector\"",
		"ROI</span>",
		"Double</span>",
		"Treble</span>",
		"Bull</span>",
		"Calibration</span>",
		"Teacher dart</span>",
		"Tip-flight line</span>",
		"Accepted throws first",
		"dart-marker",
		"teacher-detection-line",
		"teacherLinesFor",
		"detection?.imageLine",
		"l.has_coordinates?after+before",
		"Number(b.label.has_coordinates)-Number(a.label.has_coordinates)",
		"preview_darts?.after",
		"Reconstructed 3D board world",
		"Interactive realistic 3D reconstruction",
		"Drag to orbit",
		"mountWorld3D",
		"scene.board_radius_mm",
		"d.fit_residual_degrees",
		"camera.center_mm",
		"data-world-view=\"front\"",
		"data-camera-toggle",
		"sectorOrder=[20,1,18,4,13,6,10,15,2,17,3,19,7,16,8,11,14,9,12,5]",
		"[99,107,ring]",
		"[162,170,ring]",
		"needleEnd=at(28)",
		"barrelEnd=at(75)",
		"shaftEnd=at(118)",
		"updatePinch",
	} {
		if !strings.Contains(body, visibleBehavior) {
			t.Fatalf("dashboard is missing board-feature preview behavior %q", visibleBehavior)
		}
	}
}

func TestDashboardLimitsPreviewWithoutChangingTrainingSampleCount(t *testing.T) {
	output := t.TempDir()
	for index := 0; index < 3; index++ {
		sampleID := fmt.Sprintf("20260818T12000%d.000000000Z-world", index)
		directory := filepath.Join(output, sampleID)
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		label := Label{SchemaVersion: 3, SampleID: sampleID, CapturedAt: time.Date(2026, 8, 18, 12, 0, index, 0, time.UTC), LabelSource: "autodarts", Supervision: "teacher-world-state", CaptureReason: "periodic-world-observation", ReviewStatus: "unreviewed", WorldAfter: &PhysicalBoardState{Confidence: WorldConfidenceTeacher}}
		data, _ := json.Marshal(label)
		if err := os.WriteFile(filepath.Join(directory, "label.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	response := httptest.NewRecorder()
	(Dashboard{OutputDir: output}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/inventory?limit=2", nil))
	var inventory Inventory
	if err := json.Unmarshal(response.Body.Bytes(), &inventory); err != nil {
		t.Fatal(err)
	}
	if inventory.SampleCount != 3 || len(inventory.Samples) != 2 || inventory.Samples[0].Label.SampleID != "20260818T120002.000000000Z-world" {
		t.Fatalf("limited preview lost total count or newest-first ordering: %+v", inventory)
	}
}

func TestDashboardDoesNotPresentMotionWorldBeforeAsTheResultingBoard(t *testing.T) {
	dashboard := Dashboard{OutputDir: t.TempDir(), Quota: 20 << 30}
	page := httptest.NewRecorder()
	dashboard.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	body := page.Body.String()
	for _, visibleBehavior := range []string{
		"Motion settled · resulting board unknown",
		"Before motion",
		"After motion: unknown — inspect the unlabeled frames",
		"pre-settle (unlabeled)",
		"post-settle (unlabeled)",
	} {
		if !strings.Contains(body, visibleBehavior) {
			t.Fatalf("dashboard is missing motion-state explanation %q", visibleBehavior)
		}
	}
	if strings.Contains(body, "const w=world(l)") {
		t.Fatal("dashboard still collapses world_before and world_after into one ambiguous board state")
	}
}

func TestDashboardPresentsWorldObservationWithoutZeroDeltaTransition(t *testing.T) {
	dashboard := Dashboard{OutputDir: t.TempDir(), Quota: 20 << 30}
	page := httptest.NewRecorder()
	dashboard.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	body := page.Body.String()
	for _, visibleBehavior := range []string{
		"Startup observation · ",
		"Complete physical board when the recorder started",
		"Periodic stable physical-board snapshot",
		"if(t&&t.type!=='world-observed')",
	} {
		if !strings.Contains(body, visibleBehavior) {
			t.Fatalf("dashboard is missing world-observation behavior %q", visibleBehavior)
		}
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
	allFrames := append(append(append([]string{}, frames.Before...), "cam-0-middle.jpg"), frames.After...)
	for _, name := range allFrames {
		if err := os.WriteFile(filepath.Join(directory, name), []byte{0xff, 0xd8, 1, 2, 0xff, 0xd9}, 0600); err != nil {
			t.Fatal(err)
		}
	}
	teacherData := []byte(`[{"detections":[{"imageLine":{"x1":314.36,"y1":133,"x2":428.98,"y2":368}}]}]`)
	teacherName := "teacher-detections.json"
	if err := os.WriteFile(filepath.Join(directory, teacherName), teacherData, 0600); err != nil {
		t.Fatal(err)
	}
	sessionID, setupID := "session-20260813T180000Z", "0123456789abcdef"
	setupDirectory := filepath.Join(output, ".sessions", sessionID)
	if err := os.MkdirAll(setupDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	setupJSON := `{"setup_id":"0123456789abcdef","camera_resolution":{"width":1280,"height":720},"config":{"board_rois":[{"x":321,"y":189,"width":614,"height":375}],"calibration":{"0":[[0.25,0.4],[0.5,0.2],[0.75,0.45],[0.45,0.8]]},"dartboard":{"0":{"bull":[629,318],"doubleOuters":[[355,295],[401,253]],"trebleOuters":[[454,303],[479,274]]}}}}`
	if err := os.WriteFile(filepath.Join(setupDirectory, setupID+".json"), []byte(setupJSON), 0600); err != nil {
		t.Fatal(err)
	}
	sequence := make([]CapturedFrame, 0, len(allFrames))
	for index, name := range allFrames {
		frameData, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		sequence = append(sequence, CapturedFrame{Camera: index % 3, File: name, RequestedOffsetMS: int64(index * 100), SHA256: fmt.Sprintf("%x", sha256.Sum256(frameData))})
	}
	label := Label{
		SchemaVersion: SchemaVersion, SampleID: sampleID, SessionID: sessionID, SetupID: setupID,
		SetupFile: filepath.ToSlash(filepath.Join(".sessions", sessionID, setupID+".json")), CapturedAt: time.Date(2026, 8, 13, 18, 42, 5, 381000000, time.UTC),
		LabelSource: "autodarts", Supervision: "accepted-pseudo-positive", HasCoordinates: true,
		Coordinates: autodarts.Coordinates{X: 0.1277737042977262, Y: 0.1502979414527876},
		Segment:     autodarts.Segment{Name: "S18", Number: 18, Multiplier: 1, Bed: "SingleInner"}, DartIndex: 2, DartCount: 2,
		Frames: frames, FrameSequence: sequence, ReviewStatus: "unreviewed",
		TeacherArtifacts: []TeacherArtifact{{
			Kind: "detections", File: teacherName, Source: "api/state/detections", MediaType: "application/json",
			SHA256: fmt.Sprintf("%x", sha256.Sum256(teacherData)),
		}},
	}
	firstDart := PhysicalDart{ID: "dart-1", Order: 1, Coordinates: autodarts.Coordinates{X: -0.2, Y: 0.3}, Segment: autodarts.Segment{Name: "S5"}}
	secondDart := PhysicalDart{ID: "dart-2", Order: 2, Coordinates: label.Coordinates, Segment: label.Segment}
	label.WorldBefore = &PhysicalBoardState{StateID: "world-1", Confidence: WorldConfidenceTeacher, Darts: []PhysicalDart{firstDart}}
	label.WorldAfter = &PhysicalBoardState{StateID: "world-2", Confidence: WorldConfidenceTeacher, Darts: []PhysicalDart{firstDart, secondDart}}
	label.Transition = &WorldTransition{
		Type: "darts-added", Confidence: WorldConfidenceTeacher, Added: []PhysicalDart{secondDart},
		StillPresent: []PhysicalDart{firstDart},
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
	if inventory.SampleCount != 1 || inventory.EvidenceCount != 0 || inventory.Samples[0].Label.Coordinates != label.Coordinates || inventory.UsedBytes == 0 || len(inventory.Samples[0].Label.WorldAfter.Darts) != 2 || inventory.Samples[0].Label.ReviewStatus != "unreviewed" || inventory.Samples[0].PreviewDarts == nil || len(inventory.Samples[0].PreviewDarts.Before) != 1 || len(inventory.Samples[0].PreviewDarts.After) != 2 {
		t.Fatalf("unexpected populated inventory: %+v", inventory)
	}

	image := httptest.NewRecorder()
	dashboard.ServeHTTP(image, httptest.NewRequest(http.MethodGet, "/samples/"+sampleID+"/cam-0-before.jpg", nil))
	if image.Code != http.StatusOK || image.Header().Get("Content-Type") != "image/jpeg" || len(image.Body.Bytes()) != 6 {
		t.Fatalf("image status=%d type=%s bytes=%d", image.Code, image.Header().Get("Content-Type"), image.Body.Len())
	}
	teacher := httptest.NewRecorder()
	dashboard.ServeHTTP(teacher, httptest.NewRequest(http.MethodGet, "/samples/"+sampleID+"/"+teacherName, nil))
	if teacher.Code != http.StatusOK || teacher.Header().Get("Content-Type") != "application/json" || teacher.Body.String() != string(teacherData) {
		t.Fatalf("teacher artifact status=%d type=%s body=%s", teacher.Code, teacher.Header().Get("Content-Type"), teacher.Body.String())
	}

	setup := httptest.NewRecorder()
	dashboard.ServeHTTP(setup, httptest.NewRequest(http.MethodGet, "/api/setups/"+sessionID+"/"+setupID+".json", nil))
	if setup.Code != http.StatusOK || setup.Header().Get("Content-Type") != "application/json" || setup.Body.String() != setupJSON {
		t.Fatalf("setup preview status=%d type=%s body=%s", setup.Code, setup.Header().Get("Content-Type"), setup.Body.String())
	}
	unsafeSetup := httptest.NewRecorder()
	dashboard.ServeHTTP(unsafeSetup, httptest.NewRequest(http.MethodGet, "/api/setups/../"+setupID+".json", nil))
	if unsafeSetup.Code != http.StatusNotFound {
		t.Fatalf("setup traversal status=%d", unsafeSetup.Code)
	}

	traversal := httptest.NewRecorder()
	dashboard.ServeHTTP(traversal, httptest.NewRequest(http.MethodGet, "/samples/../label.json", nil))
	if traversal.Code != http.StatusNotFound {
		t.Fatalf("path traversal status=%d", traversal.Code)
	}

	export := httptest.NewRecorder()
	dashboard.ServeHTTP(export, httptest.NewRequest(http.MethodGet, "/api/export/webdataset.tar", nil))
	if export.Code != http.StatusOK || export.Header().Get("Content-Type") != "application/x-tar" || export.Header().Get("X-Autodarts-Sample-Count") != "1" {
		t.Fatalf("export status=%d type=%s count=%s", export.Code, export.Header().Get("Content-Type"), export.Header().Get("X-Autodarts-Sample-Count"))
	}
	files := map[string][]byte{}
	archive := tar.NewReader(export.Body)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		files[header.Name], err = io.ReadAll(archive)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(files) != 11 {
		t.Fatalf("exported %d files, want manifest, label, setup, seven frames, and one teacher artifact: %v", len(files), files)
	}
	var exported Label
	if err := json.Unmarshal(files[sampleID+".json"], &exported); err != nil {
		t.Fatal(err)
	}
	if exported.Coordinates != label.Coordinates || exported.Frames.Before[0] != sampleID+".cam-0-before.jpg" || exported.SetupFile != sampleID+".setup.json" || len(exported.FrameSequence) != 7 || len(exported.WorldAfter.Darts) != 2 || len(exported.Transition.StillPresent) != 1 || len(exported.TeacherArtifacts) != 1 || exported.TeacherArtifacts[0].File != sampleID+"."+teacherName {
		t.Fatalf("unexpected exported label: %+v", exported)
	}
	if string(files[sampleID+".cam-0-before.jpg"]) != string([]byte{0xff, 0xd8, 1, 2, 0xff, 0xd9}) {
		t.Fatal("exported frame differs from recorded frame")
	}
	if len(files[sampleID+".cam-0-middle.jpg"]) == 0 || len(files[sampleID+".setup.json"]) == 0 {
		t.Fatal("burst frame or setup snapshot missing from export")
	}
	if string(files[sampleID+"."+teacherName]) != string(teacherData) {
		t.Fatal("exported teacher detection lines differ from recorded artifact")
	}
	var manifest webDatasetManifest
	if err := json.Unmarshal(files["_autodarts_manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if !manifest.Complete || manifest.SampleCount != 1 || manifest.FrameCount != 7 || manifest.ArtifactCount != 1 || len(manifest.SampleIDs) != 1 || manifest.SampleIDs[0] != sampleID {
		t.Fatalf("unexpected completion manifest: %+v", manifest)
	}
}

func TestWebDatasetExportFailsBeforeDownloadWhenATeacherArtifactIsCorrupt(t *testing.T) {
	output := t.TempDir()
	sampleID := "20260819T120100.000000000Z-dart-1"
	directory := filepath.Join(output, sampleID)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	artifactName := "teacher-detections.json"
	if err := os.WriteFile(filepath.Join(directory, artifactName), []byte(`{"changed":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	label := Label{
		SchemaVersion: SchemaVersion, SampleID: sampleID, CapturedAt: time.Now().UTC(), LabelSource: "autodarts",
		Supervision: "accepted-pseudo-positive", CaptureReason: "teacher-dart-added", ReviewStatus: "unreviewed",
		TeacherArtifacts: []TeacherArtifact{{Kind: "detections", File: artifactName, Source: "api/state/detections", MediaType: "application/json", SHA256: strings.Repeat("0", 64)}},
	}
	data, _ := json.Marshal(label)
	if err := os.WriteFile(filepath.Join(directory, "label.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	(Dashboard{OutputDir: output}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/export/webdataset.tar", nil))
	if response.Code != http.StatusInternalServerError || response.Header().Get("Content-Type") == "application/x-tar" || !strings.Contains(response.Body.String(), "checksum mismatch") {
		t.Fatalf("corrupt artifact export status=%d type=%q body=%q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
}

func TestWebDatasetExportFailsBeforeDownloadWhenAFrameIsCorrupt(t *testing.T) {
	output := t.TempDir()
	sampleID := "20260819T120000.000000000Z-world"
	directory := filepath.Join(output, sampleID)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	frameName := "cam-0-000.jpg"
	if err := os.WriteFile(filepath.Join(directory, frameName), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	label := Label{
		SchemaVersion: SchemaVersion, SampleID: sampleID, CapturedAt: time.Now().UTC(), LabelSource: "autodarts",
		Supervision: "teacher-world-state", CaptureReason: "periodic-world-observation", ReviewStatus: "unreviewed",
		FrameSequence: []CapturedFrame{{Camera: 0, File: frameName, SHA256: strings.Repeat("0", 64)}},
		WorldAfter:    &PhysicalBoardState{Confidence: WorldConfidenceTeacher},
	}
	data, _ := json.Marshal(label)
	if err := os.WriteFile(filepath.Join(directory, "label.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	(Dashboard{OutputDir: output}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/export/webdataset.tar", nil))
	if response.Code != http.StatusInternalServerError || response.Header().Get("Content-Type") == "application/x-tar" || !strings.Contains(response.Body.String(), "checksum mismatch") {
		t.Fatalf("corrupt export status=%d type=%q body=%q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
}

func TestInventoryKeepsLegacyAcceptedDartVisible(t *testing.T) {
	output := t.TempDir()
	sampleID := "20260801T120000.000000000Z-dart-1"
	directory := filepath.Join(output, sampleID)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := Label{
		SchemaVersion: 1, SampleID: sampleID, CapturedAt: time.Now(), LabelSource: "autodarts",
		Coordinates: autodarts.Coordinates{X: 0.25, Y: -0.125}, Segment: autodarts.Segment{Name: "T20"},
	}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(filepath.Join(directory, "label.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	inventory, err := (Dashboard{OutputDir: output, Quota: 1024}).Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if inventory.SampleCount != 1 || !inventory.Samples[0].Label.HasCoordinates || inventory.Samples[0].Label.Supervision != "accepted-pseudo-positive" || inventory.Samples[0].Label.CaptureReason != "legacy-teacher-dart" {
		t.Fatalf("legacy accepted dart was not normalized: %+v", inventory)
	}
}

func TestInventoryExcludesLegacyMotionFromTrainingSamples(t *testing.T) {
	output := t.TempDir()
	for index, supervision := range []string{"board-reference", "unlabeled-motion"} {
		sampleID := fmt.Sprintf("20260801T12000%d.000000000Z-legacy", index)
		directory := filepath.Join(output, sampleID)
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		label := Label{SchemaVersion: 2, SampleID: sampleID, CapturedAt: time.Now().Add(time.Duration(index) * time.Second), LabelSource: "autodarts", Supervision: supervision}
		data, _ := json.Marshal(label)
		if err := os.WriteFile(filepath.Join(directory, "label.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := (Dashboard{OutputDir: output, Quota: 1024}).Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if inventory.SampleCount != 1 || inventory.EvidenceCount != 1 || inventory.EvidenceBytes == 0 || inventory.Samples[0].Label.CaptureReason != "legacy-board-reference" {
		t.Fatalf("legacy evidence was not separated from training data: %+v", inventory)
	}
}

func TestInventoryAndExportExcludeReviewRequiredEvidence(t *testing.T) {
	output := t.TempDir()
	for index, review := range []string{"unreviewed", "required"} {
		sampleID := fmt.Sprintf("20260801T13000%d.000000000Z-world", index)
		directory := filepath.Join(output, sampleID)
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		frame := "cam-0-before.jpg"
		if err := os.WriteFile(filepath.Join(directory, frame), []byte{0xff, 0xd8, 0xff, 0xd9}, 0600); err != nil {
			t.Fatal(err)
		}
		confidence := WorldConfidenceTeacher
		if review == "required" {
			confidence = WorldConfidenceReviewRequired
		}
		label := Label{SchemaVersion: 3, SampleID: sampleID, CapturedAt: time.Now().Add(time.Duration(index) * time.Second), LabelSource: "autodarts", Supervision: "teacher-world-state", CaptureReason: "periodic-world-observation", ReviewStatus: review, Frames: FrameFiles{Before: []string{frame}}, WorldAfter: &PhysicalBoardState{Confidence: confidence}, Transition: &WorldTransition{Type: "world-observed", Confidence: confidence}}
		data, _ := json.Marshal(label)
		if err := os.WriteFile(filepath.Join(directory, "label.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	dashboard := Dashboard{OutputDir: output, Quota: 1024}
	inventory, err := dashboard.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if inventory.SampleCount != 1 || inventory.EvidenceCount != 1 || len(inventory.Samples) != 1 {
		t.Fatalf("review-required evidence entered training inventory: %+v", inventory)
	}
	export := httptest.NewRecorder()
	dashboard.ServeHTTP(export, httptest.NewRequest(http.MethodGet, "/api/export/webdataset.tar", nil))
	if export.Header().Get("X-Autodarts-Sample-Count") != "1" {
		t.Fatalf("review-required evidence entered export: count=%s", export.Header().Get("X-Autodarts-Sample-Count"))
	}
}
