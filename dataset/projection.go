package dataset

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"

	"github.com/jonaswre/autodarts-nixos/autodarts"
)

type PreviewDart struct {
	Camera  int     `json:"camera"`
	ID      string  `json:"id"`
	Order   int     `json:"order"`
	Segment string  `json:"segment"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
}

type PreviewDarts struct {
	Before []PreviewDart `json:"before,omitempty"`
	After  []PreviewDart `json:"after,omitempty"`
}

type previewSetup struct {
	width       float64
	height      float64
	calibration map[string][][]float64
	dartboard   map[string]struct {
		DoubleOuters [][]float64 `json:"doubleOuters"`
	}
}

func loadPreviewSetup(outputDir string, label Label) (*previewSetup, error) {
	if !safeName(label.SessionID) || !safeName(label.SetupID) {
		return nil, errors.New("sample has no safe setup identity")
	}
	data, err := os.ReadFile(filepath.Join(outputDir, ".sessions", label.SessionID, label.SetupID+".json"))
	if err != nil {
		return nil, err
	}
	var snapshot struct {
		CameraResolution autodarts.Resolution `json:"camera_resolution"`
		Config           json.RawMessage      `json:"config"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	var config struct {
		Calibration map[string][][]float64 `json:"calibration"`
		Dartboard   map[string]struct {
			DoubleOuters [][]float64 `json:"doubleOuters"`
		} `json:"dartboard"`
		Camera struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"cam"`
	}
	if err := json.Unmarshal(snapshot.Config, &config); err != nil {
		return nil, err
	}
	width, height := snapshot.CameraResolution.Width, snapshot.CameraResolution.Height
	if width == 0 || height == 0 {
		width, height = config.Camera.Width, config.Camera.Height
	}
	if width <= 0 || height <= 0 {
		return nil, errors.New("setup has no camera resolution")
	}
	return &previewSetup{width: float64(width), height: float64(height), calibration: config.Calibration, dartboard: config.Dartboard}, nil
}

func (s *previewSetup) project(camera int, coordinates autodarts.Coordinates) (float64, float64, bool) {
	destination := s.calibration[strconv.Itoa(camera)]
	if len(destination) < 4 {
		outer := s.dartboard[strconv.Itoa(camera)].DoubleOuters
		if len(outer) >= 16 {
			destination = [][]float64{outer[0], outer[5], outer[10], outer[15]}
		}
	}
	if len(destination) < 4 {
		return 0, 0, false
	}
	var destinationPoints [4][2]float64
	for index := range destinationPoints {
		if len(destination[index]) < 2 {
			return 0, 0, false
		}
		x, y := destination[index][0], destination[index][1]
		if math.Abs(x) <= 2 && math.Abs(y) <= 2 {
			x, y = x*s.width, y*s.height
		}
		destinationPoints[index] = [2]float64{x, y}
	}
	homography, ok := solveHomography(calibrationBoardPoints(), destinationPoints)
	if !ok {
		return 0, 0, false
	}
	denominator := homography[6]*coordinates.X + homography[7]*coordinates.Y + 1
	if math.Abs(denominator) < 1e-12 {
		return 0, 0, false
	}
	x := (homography[0]*coordinates.X + homography[1]*coordinates.Y + homography[2]) / denominator
	y := (homography[3]*coordinates.X + homography[4]*coordinates.Y + homography[5]) / denominator
	return x, y, !math.IsNaN(x) && !math.IsNaN(y) && !math.IsInf(x, 0) && !math.IsInf(y, 0)
}

func calibrationBoardPoints() [4][2]float64 {
	var points [4][2]float64
	for index := range points {
		angle := float64(9+index*90) * math.Pi / 180
		points[index] = [2]float64{math.Sin(angle), math.Cos(angle)}
	}
	return points
}

func solveHomography(source, destination [4][2]float64) ([8]float64, bool) {
	var matrix [8][9]float64
	for index := 0; index < 4; index++ {
		x, y := source[index][0], source[index][1]
		u, v := destination[index][0], destination[index][1]
		matrix[index*2] = [9]float64{x, y, 1, 0, 0, 0, -u * x, -u * y, u}
		matrix[index*2+1] = [9]float64{0, 0, 0, x, y, 1, -v * x, -v * y, v}
	}
	for column := 0; column < 8; column++ {
		pivot := column
		for row := column + 1; row < 8; row++ {
			if math.Abs(matrix[row][column]) > math.Abs(matrix[pivot][column]) {
				pivot = row
			}
		}
		if math.Abs(matrix[pivot][column]) < 1e-12 {
			return [8]float64{}, false
		}
		matrix[column], matrix[pivot] = matrix[pivot], matrix[column]
		factor := matrix[column][column]
		for value := column; value < 9; value++ {
			matrix[column][value] /= factor
		}
		for row := 0; row < 8; row++ {
			if row == column {
				continue
			}
			factor = matrix[row][column]
			for value := column; value < 9; value++ {
				matrix[row][value] -= factor * matrix[column][value]
			}
		}
	}
	var result [8]float64
	for index := range result {
		result[index] = matrix[index][8]
	}
	return result, true
}

func projectPreviewDarts(setup *previewSetup, darts []PhysicalDart) []PreviewDart {
	if setup == nil || len(darts) == 0 {
		return nil
	}
	result := make([]PreviewDart, 0, len(darts)*3)
	for camera := 0; camera < 3; camera++ {
		for _, dart := range darts {
			x, y, ok := setup.project(camera, dart.Coordinates)
			if !ok {
				continue
			}
			result = append(result, PreviewDart{Camera: camera, ID: dart.ID, Order: dart.Order, Segment: dart.Segment.Name, X: x, Y: y})
		}
	}
	return result
}
