package dataset

import (
	"math"
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
