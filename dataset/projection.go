package dataset

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
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

type Preview3D struct {
	Units         string            `json:"units"`
	Method        string            `json:"method"`
	BoardRadiusMM float64           `json:"board_radius_mm"`
	Darts         []Preview3DDart   `json:"darts"`
	Cameras       []Preview3DCamera `json:"cameras"`
}

type Preview3DDart struct {
	ID                 string     `json:"id"`
	Order              int        `json:"order"`
	Segment            string     `json:"segment"`
	TipMM              [3]float64 `json:"tip_mm"`
	FlightMM           [3]float64 `json:"flight_mm"`
	Direction          [3]float64 `json:"direction"`
	LengthMM           float64    `json:"length_mm"`
	FitResidualDegrees float64    `json:"fit_residual_degrees"`
	CameraLines        int        `json:"camera_lines"`
}

type Preview3DCamera struct {
	Camera   int        `json:"camera"`
	CenterMM [3]float64 `json:"center_mm"`
}

type cameraIntrinsics struct {
	K [][]float64 `json:"K"`
}

type previewSetup struct {
	width       float64
	height      float64
	calibration map[string][][]float64
	distortion  map[string]cameraIntrinsics
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
		Calibration map[string][][]float64      `json:"calibration"`
		Distortion  map[string]cameraIntrinsics `json:"distortion"`
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
	return &previewSetup{width: float64(width), height: float64(height), calibration: config.Calibration, distortion: config.Distortion, dartboard: config.Dartboard}, nil
}

func (s *previewSetup) project(camera int, coordinates autodarts.Coordinates) (float64, float64, bool) {
	homography, ok := s.homography(camera)
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

func (s *previewSetup) homography(camera int) ([8]float64, bool) {
	destination := s.calibration[strconv.Itoa(camera)]
	if len(destination) < 4 {
		outer := s.dartboard[strconv.Itoa(camera)].DoubleOuters
		if len(outer) >= 16 {
			destination = [][]float64{outer[0], outer[5], outer[10], outer[15]}
		}
	}
	if len(destination) < 4 {
		return [8]float64{}, false
	}
	var destinationPoints [4][2]float64
	for index := range destinationPoints {
		if len(destination[index]) < 2 {
			return [8]float64{}, false
		}
		x, y := destination[index][0], destination[index][1]
		if math.Abs(x) <= 2 && math.Abs(y) <= 2 {
			x, y = x*s.width, y*s.height
		}
		destinationPoints[index] = [2]float64{x, y}
	}
	homography, ok := solveHomography(calibrationBoardPoints(), destinationPoints)
	if !ok {
		return [8]float64{}, false
	}
	return homography, true
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

func reconstructPreview3D(outputDir string, label Label, setup *previewSetup) *Preview3D {
	if setup == nil || !label.HasCoordinates {
		return nil
	}
	artifactFile := ""
	for _, artifact := range label.TeacherArtifacts {
		if artifact.Kind == "detections" && safeArtifactName(artifact.File) {
			artifactFile = artifact.File
			break
		}
	}
	if artifactFile == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(outputDir, label.SampleID, artifactFile))
	if err != nil {
		return nil
	}
	type imageLine struct {
		X1 float64 `json:"x1"`
		Y1 float64 `json:"y1"`
		X2 float64 `json:"x2"`
		Y2 float64 `json:"y2"`
	}
	type detectionResult struct {
		Detections []struct {
			ImageLine imageLine `json:"imageLine"`
		} `json:"detections"`
		Coordinates autodarts.Coordinates `json:"coords"`
	}
	var results []detectionResult
	if json.Unmarshal(data, &results) != nil || len(results) == 0 {
		return nil
	}
	type cameraPose struct {
		camera             int
		fx, fy, cx, cy     float64
		center, r1, r2, r3 [3]float64
	}
	poses := make(map[int]cameraPose)
	for camera := 0; camera < 3; camera++ {
		h, ok := setup.homography(camera)
		intrinsics := setup.distortion[strconv.Itoa(camera)].K
		if !ok || !validCameraMatrix(intrinsics) {
			continue
		}
		fx, fy := intrinsics[0][0], intrinsics[1][1]
		cx, cy := intrinsics[0][2], intrinsics[1][2]
		kinv := func(value [3]float64) [3]float64 {
			return [3]float64{(value[0] - cx*value[2]) / fx, (value[1] - cy*value[2]) / fy, value[2]}
		}
		h1 := kinv([3]float64{h[0], h[3], h[6]})
		h2 := kinv([3]float64{h[1], h[4], h[7]})
		h3 := kinv([3]float64{h[2], h[5], 1})
		scale := 2 / (vectorLength(h1) + vectorLength(h2))
		r1 := normalizeVector(scaleVector(h1, scale))
		r2raw := scaleVector(h2, scale)
		r2 := normalizeVector(subtractVector(r2raw, scaleVector(r1, dotVector(r1, r2raw))))
		r3 := normalizeVector(crossVector(r1, r2))
		t := scaleVector(h3, scale)
		center := [3]float64{-dotVector(r1, t), -dotVector(r2, t), -dotVector(r3, t)}
		poses[camera] = cameraPose{camera: camera, fx: fx, fy: fy, cx: cx, cy: cy, center: center, r1: r1, r2: r2, r3: r3}
	}
	if len(poses) < 2 {
		return nil
	}
	frontSign := 1.0
	averageCameraZ := 0.0
	for _, pose := range poses {
		averageCameraZ += pose.center[2]
	}
	if averageCameraZ < 0 {
		frontSign = -1
	}

	const boardRadiusMM = 170.0
	const visibleDartLengthMM = 140.0
	cameras := make([]Preview3DCamera, 0, len(poses))
	for camera := 0; camera < 3; camera++ {
		pose, ok := poses[camera]
		if !ok {
			continue
		}
		center := scaleVector(pose.center, boardRadiusMM)
		center[2] *= frontSign
		cameras = append(cameras, Preview3DCamera{Camera: camera, CenterMM: center})
	}
	physicalDarts := []PhysicalDart{}
	if label.WorldAfter != nil {
		physicalDarts = label.WorldAfter.Darts
	}
	usedPhysicalDarts := make(map[int]bool)
	darts := make([]Preview3DDart, 0, len(results))
	for resultIndex, result := range results {
		normals := make([][3]float64, 0, len(result.Detections))
		for camera, detection := range result.Detections {
			pose, ok := poses[camera]
			if !ok {
				continue
			}
			p1 := [3]float64{detection.ImageLine.X1, detection.ImageLine.Y1, 1}
			p2 := [3]float64{detection.ImageLine.X2, detection.ImageLine.Y2, 1}
			pixelLine := crossVector(p1, p2)
			cameraNormal := [3]float64{pose.fx * pixelLine[0], pose.fy * pixelLine[1], pose.cx*pixelLine[0] + pose.cy*pixelLine[1] + pixelLine[2]}
			worldNormal := normalizeVector([3]float64{dotVector(pose.r1, cameraNormal), dotVector(pose.r2, cameraNormal), dotVector(pose.r3, cameraNormal)})
			if vectorLength(worldNormal) > 0 {
				normals = append(normals, worldNormal)
			}
		}
		if len(normals) < 2 {
			continue
		}
		direction := smallestEigenvector(normals)
		if vectorLength(direction) == 0 {
			continue
		}
		if direction[2]*frontSign < 0 {
			direction = scaleVector(direction, -1)
		}
		residualSquares := 0.0
		for _, normal := range normals {
			value := math.Min(1, math.Abs(dotVector(normal, direction)))
			angle := math.Asin(value) * 180 / math.Pi
			residualSquares += angle * angle
		}
		direction[2] *= frontSign

		physical := PhysicalDart{ID: label.SampleID, Order: resultIndex + 1, Coordinates: result.Coordinates, Segment: label.Segment}
		bestPhysical, bestDistance := -1, math.Inf(1)
		for index, candidate := range physicalDarts {
			if usedPhysicalDarts[index] {
				continue
			}
			dx, dy := candidate.Coordinates.X-result.Coordinates.X, candidate.Coordinates.Y-result.Coordinates.Y
			if distance := dx*dx + dy*dy; distance < bestDistance {
				bestPhysical, bestDistance = index, distance
			}
		}
		if bestPhysical >= 0 {
			physical = physicalDarts[bestPhysical]
			usedPhysicalDarts[bestPhysical] = true
		} else if len(results) == 1 {
			physical.Coordinates, physical.Order = label.Coordinates, label.DartIndex
			if label.Transition != nil && len(label.Transition.Added) > 0 {
				physical.ID = label.Transition.Added[0].ID
			}
		}
		tip := [3]float64{physical.Coordinates.X * boardRadiusMM, physical.Coordinates.Y * boardRadiusMM, 0}
		darts = append(darts, Preview3DDart{
			ID: physical.ID, Order: physical.Order, Segment: physical.Segment.Name,
			TipMM: tip, FlightMM: addVector(tip, scaleVector(direction, visibleDartLengthMM)), Direction: direction, LengthMM: visibleDartLengthMM,
			FitResidualDegrees: math.Sqrt(residualSquares / float64(len(normals))), CameraLines: len(normals),
		})
	}
	if len(darts) == 0 {
		return nil
	}
	sort.Slice(darts, func(i, j int) bool { return darts[i].Order < darts[j].Order })
	return &Preview3D{
		Units: "mm", Method: "calibrated image-line plane least squares", BoardRadiusMM: boardRadiusMM,
		Darts: darts, Cameras: cameras,
	}
}

func validCameraMatrix(matrix [][]float64) bool {
	return len(matrix) >= 3 && len(matrix[0]) >= 3 && len(matrix[1]) >= 3 && matrix[0][0] > 0 && matrix[1][1] > 0
}

func addVector(left, right [3]float64) [3]float64 {
	return [3]float64{left[0] + right[0], left[1] + right[1], left[2] + right[2]}
}

func subtractVector(left, right [3]float64) [3]float64 {
	return [3]float64{left[0] - right[0], left[1] - right[1], left[2] - right[2]}
}

func scaleVector(value [3]float64, scale float64) [3]float64 {
	return [3]float64{value[0] * scale, value[1] * scale, value[2] * scale}
}

func dotVector(left, right [3]float64) float64 {
	return left[0]*right[0] + left[1]*right[1] + left[2]*right[2]
}

func crossVector(left, right [3]float64) [3]float64 {
	return [3]float64{left[1]*right[2] - left[2]*right[1], left[2]*right[0] - left[0]*right[2], left[0]*right[1] - left[1]*right[0]}
}

func vectorLength(value [3]float64) float64 {
	return math.Sqrt(dotVector(value, value))
}

func normalizeVector(value [3]float64) [3]float64 {
	length := vectorLength(value)
	if length < 1e-12 {
		return [3]float64{}
	}
	return scaleVector(value, 1/length)
}

func smallestEigenvector(normals [][3]float64) [3]float64 {
	var matrix [3][3]float64
	for _, normal := range normals {
		for row := 0; row < 3; row++ {
			for column := 0; column < 3; column++ {
				matrix[row][column] += normal[row] * normal[column]
			}
		}
	}
	vectors := [3][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	for iteration := 0; iteration < 24; iteration++ {
		p, q := 0, 1
		for _, pair := range [][2]int{{0, 2}, {1, 2}} {
			if math.Abs(matrix[pair[0]][pair[1]]) > math.Abs(matrix[p][q]) {
				p, q = pair[0], pair[1]
			}
		}
		if math.Abs(matrix[p][q]) < 1e-12 {
			break
		}
		angle := .5 * math.Atan2(2*matrix[p][q], matrix[q][q]-matrix[p][p])
		cosine, sine := math.Cos(angle), math.Sin(angle)
		for row := 0; row < 3; row++ {
			left, right := matrix[row][p], matrix[row][q]
			matrix[row][p], matrix[row][q] = cosine*left-sine*right, sine*left+cosine*right
		}
		for column := 0; column < 3; column++ {
			top, bottom := matrix[p][column], matrix[q][column]
			matrix[p][column], matrix[q][column] = cosine*top-sine*bottom, sine*top+cosine*bottom
		}
		for row := 0; row < 3; row++ {
			left, right := vectors[row][p], vectors[row][q]
			vectors[row][p], vectors[row][q] = cosine*left-sine*right, sine*left+cosine*right
		}
	}
	index := 0
	if matrix[1][1] < matrix[index][index] {
		index = 1
	}
	if matrix[2][2] < matrix[index][index] {
		index = 2
	}
	return normalizeVector([3]float64{vectors[0][index], vectors[1][index], vectors[2][index]})
}
