package dataset

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/jonaswre/autodarts-nixos/autodarts"
)

func TestHomographyProjectsBoardCoordinateIntoCameraPixel(t *testing.T) {
	source := calibrationBoardPoints()
	wantTransform := [8]float64{410, 35, 640, -28, -305, 360, 0.08, -0.04}
	var destination [4][2]float64
	for index, point := range source {
		destination[index] = applyHomographyForTest(wantTransform, point)
	}
	gotTransform, ok := solveHomography(source, destination)
	if !ok {
		t.Fatal("calibration landmarks did not produce a homography")
	}
	boardPoint := [2]float64{-0.3207565459573911, -0.15168396522505417}
	want := applyHomographyForTest(wantTransform, boardPoint)
	got := applyHomographyForTest(gotTransform, boardPoint)
	if math.Abs(got[0]-want[0]) > 1e-8 || math.Abs(got[1]-want[1]) > 1e-8 {
		t.Fatalf("projected pixel = %v, want %v", got, want)
	}
}

func TestThreeCameraLinesReconstructPhysicalDartInMillimetres(t *testing.T) {
	setup := &previewSetup{
		width: 1280, height: 720,
		calibration: map[string][][]float64{
			"0": {{353.6228467686617, 294.68534384199984}, {662.1150171754298, 189.07625722938235}, {931.3959856334609, 342.9165106038098}, {563.5964002025205, 559.0395476576359}},
			"1": {{780.7460652084537, 234.32832055977576}, {906.2848432436376, 464.5048746245051}, {387.77666404533875, 497.6644275360493}, {450.01031996312423, 245.4039445774958}},
			"2": {{794.7921374275373, 530.8783155657723}, {332.5495418434752, 376.0743096350558}, {558.5633011075547, 184.0639035802468}, {887.2141938797059, 248.44851645619917}},
		},
		distortion: map[string]cameraIntrinsics{},
	}
	for camera := 0; camera < 3; camera++ {
		setup.distortion[string(rune('0'+camera))] = cameraIntrinsics{K: [][]float64{{712.0206848692635, 0, 640}, {0, 712.0206848692635, 360}, {0, 0, 1}}}
	}
	coordinates := autodarts.Coordinates{X: -0.11349135798374649, Y: 0.2513658763099136}
	detections := []map[string]any{{
		"coords": coordinates,
		"detections": []map[string]any{
			{"imageLine": map[string]float64{"x1": 473.66862290247127, "y1": 121, "x2": 553.7420946806719, "y2": 341}},
			{"imageLine": map[string]float64{"x1": 654.0384256900828, "y1": 79, "x2": 642.6135294878713, "y2": 297}},
			{"imageLine": map[string]float64{"x1": 779.705280360199, "y1": 120, "x2": 708.8727844744063, "y2": 338}},
		},
	}, {
		"coords": autodarts.Coordinates{X: 0.2, Y: -0.1},
		"detections": []map[string]any{
			{"imageLine": map[string]float64{"x1": 410, "y1": 115, "x2": 500, "y2": 350}},
			{"imageLine": map[string]float64{"x1": 700, "y1": 90, "x2": 680, "y2": 315}},
			{"imageLine": map[string]float64{"x1": 750, "y1": 105, "x2": 690, "y2": 345}},
		},
	}}
	data, err := json.Marshal(detections)
	if err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	sampleID := "20260819T065517.076881210Z-dart-1"
	directory := filepath.Join(output, sampleID)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "teacher-detections.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	label := Label{
		SampleID: sampleID, HasCoordinates: true, Coordinates: coordinates, Segment: autodarts.Segment{Name: "S5"}, DartIndex: 1,
		TeacherArtifacts: []TeacherArtifact{{Kind: "detections", File: "teacher-detections.json"}},
		Transition:       &WorldTransition{Added: []PhysicalDart{{ID: "dart-1"}}},
		WorldAfter: &PhysicalBoardState{Darts: []PhysicalDart{
			{ID: "dart-1", Order: 1, Coordinates: coordinates, Segment: autodarts.Segment{Name: "S5"}},
			{ID: "dart-2", Order: 2, Coordinates: autodarts.Coordinates{X: 0.2, Y: -0.1}, Segment: autodarts.Segment{Name: "S13"}},
		}},
	}
	preview := reconstructPreview3D(output, label, setup)
	if preview == nil || preview.Units != "mm" || preview.BoardRadiusMM != 170 || len(preview.Darts) != 2 || len(preview.Cameras) != 3 {
		t.Fatalf("unexpected 3D reconstruction: %+v", preview)
	}
	dart := preview.Darts[0]
	if dart.CameraLines != 3 || dart.TipMM != [3]float64{coordinates.X * 170, coordinates.Y * 170, 0} || dart.FlightMM[2] <= 0 || math.Abs(vectorLength(dart.Direction)-1) > 1e-9 || math.IsNaN(dart.FitResidualDegrees) || dart.FitResidualDegrees > 5 {
		t.Fatalf("invalid reconstructed S5 dart: %+v", dart)
	}
	if preview.Darts[1].ID != "dart-2" || preview.Darts[1].Order != 2 || preview.Darts[1].Segment != "S13" || preview.Darts[1].TipMM != [3]float64{34, -17, 0} {
		t.Fatalf("complete physical world did not retain second dart: %+v", preview.Darts)
	}
}

func TestPreviewProjectsEveryTeacherDartIntoAllThreeCameras(t *testing.T) {
	landmarks := calibrationBoardPoints()
	setup := &previewSetup{width: 1280, height: 720, calibration: map[string][][]float64{}}
	for camera := 0; camera < 3; camera++ {
		points := make([][]float64, 4)
		for index, point := range landmarks {
			points[index] = []float64{(point[0]*0.25 + 0.5), (0.5 - point[1]*0.25)}
		}
		setup.calibration[string(rune('0'+camera))] = points
	}
	darts := []PhysicalDart{
		{ID: "dart-1", Order: 1, Coordinates: autodarts.Coordinates{X: 0, Y: 0}, Segment: autodarts.Segment{Name: "BULL"}},
		{ID: "dart-2", Order: 2, Coordinates: autodarts.Coordinates{X: 0.25, Y: -0.5}, Segment: autodarts.Segment{Name: "S8"}},
	}
	projected := projectPreviewDarts(setup, darts)
	if len(projected) != 6 {
		t.Fatalf("projected %d dart markers, want two in each camera: %+v", len(projected), projected)
	}
	for camera := 0; camera < 3; camera++ {
		first := projected[camera*2]
		if first.Camera != camera || first.Order != 1 || first.Segment != "BULL" || math.Abs(first.X-640) > 1e-8 || math.Abs(first.Y-360) > 1e-8 {
			t.Fatalf("camera %d bull marker = %+v", camera, first)
		}
	}
}

func applyHomographyForTest(h [8]float64, point [2]float64) [2]float64 {
	denominator := h[6]*point[0] + h[7]*point[1] + 1
	return [2]float64{
		(h[0]*point[0] + h[1]*point[1] + h[2]) / denominator,
		(h[3]*point[0] + h[4]*point[1] + h[5]) / denominator,
	}
}
