package dataset

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jonaswre/autodarts-nixos/autodarts"
)

const SchemaVersion = 3

const maxFrameSkew = 250 * time.Millisecond

type Label struct {
	SchemaVersion  int                   `json:"schema_version"`
	SampleID       string                `json:"sample_id"`
	SessionID      string                `json:"session_id,omitempty"`
	SetupID        string                `json:"setup_id,omitempty"`
	SetupFile      string                `json:"setup_file,omitempty"`
	CapturedAt     time.Time             `json:"captured_at"`
	LabelSource    string                `json:"label_source"`
	Supervision    string                `json:"supervision,omitempty"`
	CaptureReason  string                `json:"capture_reason,omitempty"`
	HasCoordinates bool                  `json:"has_coordinates"`
	Coordinates    autodarts.Coordinates `json:"coordinates"`
	Segment        autodarts.Segment     `json:"segment"`
	DartIndex      int                   `json:"dart_index"`
	DartCount      int                   `json:"dart_count"`
	Frames         FrameFiles            `json:"frames"`
	FrameSequence  []CapturedFrame       `json:"frame_sequence,omitempty"`
	WorldBefore    *PhysicalBoardState   `json:"world_before,omitempty"`
	WorldAfter     *PhysicalBoardState   `json:"world_after,omitempty"`
	Transition     *WorldTransition      `json:"transition,omitempty"`
	Context        CaptureContext        `json:"context,omitempty"`
	ReviewStatus   string                `json:"review_status"`
	ReviewReasons  []string              `json:"review_reasons,omitempty"`
}

type FrameFiles struct {
	Before []string `json:"before"`
	After  []string `json:"after"`
}

type CapturedFrame struct {
	Camera            int       `json:"camera"`
	File              string    `json:"file"`
	RequestedOffsetMS int64     `json:"requested_offset_ms"`
	CapturedAt        time.Time `json:"captured_at"`
	ActualOffsetMS    int64     `json:"actual_offset_ms"`
	SHA256            string    `json:"sha256"`
	Role              string    `json:"role,omitempty"`
	WorldState        string    `json:"world_state,omitempty"`
}

type RecordedEvent struct {
	ReceivedAt time.Time       `json:"received_at"`
	Type       string          `json:"type"`
	Data       json.RawMessage `json:"data"`
}

type CaptureContext struct {
	BoardState   autodarts.BoardState     `json:"board_state"`
	RecentEvents []RecordedEvent          `json:"recent_events,omitempty"`
	LatestEvents map[string]RecordedEvent `json:"latest_events,omitempty"`
}

type SetupSnapshot struct {
	SchemaVersion       int                      `json:"schema_version"`
	SessionID           string                   `json:"session_id"`
	SetupID             string                   `json:"setup_id"`
	CapturedAt          time.Time                `json:"captured_at"`
	BoardManagerVersion string                   `json:"board_manager_version,omitempty"`
	Host                SetupHost                `json:"host,omitempty"`
	CameraState         autodarts.CameraState    `json:"camera_state"`
	CameraStats         autodarts.CameraStats    `json:"camera_stats"`
	DetectionStats      autodarts.DetectionStats `json:"detection_stats"`
	CameraResolution    autodarts.Resolution     `json:"camera_resolution"`
	DeviceFingerprints  []string                 `json:"device_fingerprints,omitempty"`
	Config              json.RawMessage          `json:"config,omitempty"`
	Calibration         json.RawMessage          `json:"calibration,omitempty"`
	Distortion          json.RawMessage          `json:"distortion,omitempty"`
	CalibrationEllipses json.RawMessage          `json:"calibration_ellipses,omitempty"`
}

type SetupHost struct {
	ClientVersion   string `json:"client_version,omitempty"`
	OS              string `json:"os,omitempty"`
	Platform        string `json:"platform,omitempty"`
	PlatformVersion string `json:"platform_version,omitempty"`
	KernelArch      string `json:"kernel_arch,omitempty"`
	KernelVersion   string `json:"kernel_version,omitempty"`
}

type Options struct {
	BoardURL          string
	OutputDir         string
	Cameras           int
	PreDelay          time.Duration
	PostDelay         time.Duration
	BurstOffsets      []time.Duration
	ReferenceInterval time.Duration
	Quota             int64
	Catalog           *Catalog
	Client            *http.Client
	Now               func() time.Time
	OnSample          func(path string, label Label)
	OnError           func(error)
}

type frame struct {
	at   time.Time
	jpeg []byte
}

type cameraBuffer struct {
	mu     sync.RWMutex
	frames []frame
}

func (b *cameraBuffer) add(value frame) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.frames = append(b.frames, value)
	cutoff := value.at.Add(-5 * time.Second)
	for len(b.frames) > 1 && b.frames[0].at.Before(cutoff) {
		b.frames = b.frames[1:]
	}
}

func (b *cameraBuffer) closest(target time.Time) (frame, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.frames) == 0 {
		return frame{}, false
	}
	best := b.frames[0]
	bestDistance := absDuration(best.at.Sub(target))
	for _, candidate := range b.frames[1:] {
		distance := absDuration(candidate.at.Sub(target))
		if distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	best.jpeg = append([]byte(nil), best.jpeg...)
	return best, true
}

func (b *cameraBuffer) covers(start, end time.Time) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.frames) >= 2 && !b.frames[0].at.After(start) && !b.frames[len(b.frames)-1].at.Before(end)
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

type Recorder struct {
	options      Options
	client       *autodarts.Client
	buffers      []cameraBuffer
	mu           sync.RWMutex
	sessionID    string
	setup        SetupSnapshot
	recentEvents []RecordedEvent
	latestEvents map[string]RecordedEvent
	motionActive bool
	captureWG    sync.WaitGroup
	quotaMu      sync.Mutex
}

func New(options Options) (*Recorder, error) {
	if options.BoardURL == "" || options.OutputDir == "" {
		return nil, errors.New("board URL and output directory are required")
	}
	if options.Cameras == 0 {
		options.Cameras = 3
	}
	if options.PreDelay == 0 {
		options.PreDelay = 250 * time.Millisecond
	}
	if options.PostDelay == 0 {
		options.PostDelay = 350 * time.Millisecond
	}
	if len(options.BurstOffsets) == 0 {
		options.BurstOffsets = []time.Duration{-750 * time.Millisecond, -500 * time.Millisecond, -250 * time.Millisecond, -100 * time.Millisecond, 0, 150 * time.Millisecond, 350 * time.Millisecond, 750 * time.Millisecond}
	}
	if options.ReferenceInterval == 0 {
		options.ReferenceInterval = 5 * time.Minute
	}
	if options.ReferenceInterval < 0 {
		return nil, errors.New("reference interval cannot be negative")
	}
	options.BurstOffsets = normalizedOffsets(options.BurstOffsets, -options.PreDelay, options.PostDelay)
	if options.Quota == 0 {
		options.Quota = 20 << 30
	}
	if options.Client == nil {
		options.Client = &http.Client{}
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	client, err := autodarts.New(options.BoardURL, autodarts.WithHTTPClient(options.Client))
	if err != nil {
		return nil, err
	}
	return &Recorder{options: options, client: client, buffers: make([]cameraBuffer, options.Cameras), latestEvents: map[string]RecordedEvent{}}, nil
}

func (r *Recorder) waitForCameraHistory(ctx context.Context) error {
	oldestOffset := r.options.BurstOffsets[0]
	if oldestOffset > 0 {
		oldestOffset = 0
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		now := r.options.Now().UTC()
		start := now.Add(oldestOffset)
		end := now.Add(-maxFrameSkew)
		ready := true
		for index := range r.buffers {
			if !r.buffers[index].covers(start, end) {
				ready = false
				break
			}
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func normalizedOffsets(offsets []time.Duration, required ...time.Duration) []time.Duration {
	seen := map[time.Duration]bool{}
	result := make([]time.Duration, 0, len(offsets)+len(required))
	for _, offset := range append(append([]time.Duration{}, offsets...), required...) {
		if !seen[offset] {
			seen[offset] = true
			result = append(result, offset)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func (r *Recorder) Run(ctx context.Context) error {
	if err := os.MkdirAll(r.options.OutputDir, 0700); err != nil {
		return err
	}
	r.sessionID = "session-" + r.options.Now().UTC().Format("20060102T150405.000000000Z")
	if err := r.refreshSetup(ctx); err != nil && r.options.OnError != nil {
		r.options.OnError(fmt.Errorf("capture setup metadata: %w", err))
	}
	for camera := range r.buffers {
		go r.readCamera(ctx, camera)
	}
	if err := r.waitForCameraHistory(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("prime camera history: %w", err)
	}
	events, failures, err := r.client.Events(ctx)
	if err != nil {
		return fmt.Errorf("subscribe to board events: %w", err)
	}
	initial, err := r.waitForStableBoardState(ctx, events, failures)
	if err != nil {
		return fmt.Errorf("wait for stable initial board state: %w", err)
	}
	tracker := newWorldTracker(r.sessionID, initial, r.options.Now().UTC())
	startupAt := r.options.Now().UTC()
	startupWorld := tracker.snapshot(startupAt, WorldConfidenceTeacher)
	r.scheduleCapture(ctx, startupAt, "world-startup", Label{
		LabelSource: "autodarts", Supervision: "teacher-world-state", CaptureReason: "startup-world-observation",
		WorldAfter: worldPointer(startupWorld), Transition: &WorldTransition{Type: "world-observed", Source: "autodarts-state", Confidence: WorldConfidenceTeacher},
		Context: CaptureContext{BoardState: initial},
	})
	defer r.captureWG.Wait()
	references := time.NewTicker(r.options.ReferenceInterval)
	defer references.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-failures:
			if err != nil && ctx.Err() == nil {
				return fmt.Errorf("board events: %w", err)
			}
			return nil
		case event, ok := <-events:
			if !ok {
				return nil
			}
			receivedAt := r.options.Now().UTC()
			r.rememberEvent(receivedAt, event)
			if event.Type != "state" {
				continue
			}
			var state autodarts.BoardState
			if err := json.Unmarshal(event.Data, &state); err != nil {
				continue
			}
			before, after, transition, changed := tracker.observe(state, receivedAt, WorldConfidenceTeacher)
			if changed {
				if len(transition.Added) > 0 {
					for _, dart := range transition.Added {
						transitionCopy := transition
						r.scheduleCapture(ctx, receivedAt, fmt.Sprintf("dart-%d", dart.Order), Label{
							LabelSource: "autodarts", Supervision: "accepted-pseudo-positive", CaptureReason: "teacher-dart-added", HasCoordinates: true,
							Coordinates: dart.Coordinates, Segment: dart.Segment, DartIndex: dart.Order, DartCount: len(after.Darts),
							WorldBefore: worldPointer(before), WorldAfter: worldPointer(after), Transition: &transitionCopy,
							Context: CaptureContext{BoardState: state},
						})
					}
				} else {
					supervision := "teacher-world-state"
					if len(after.Darts) == 0 {
						supervision = "board-reference"
					}
					transitionCopy := transition
					r.scheduleCapture(ctx, receivedAt, "world-reconciled", Label{
						LabelSource: "autodarts", Supervision: supervision, CaptureReason: "teacher-world-reconciled",
						WorldBefore: worldPointer(before), WorldAfter: worldPointer(after), Transition: &transitionCopy,
						Context: CaptureContext{BoardState: state},
					})
				}
			}
			if strings.Contains(strings.ToLower(state.Event), "calibration finished") {
				go func() {
					if err := r.refreshSetup(ctx); err != nil && r.options.OnError != nil {
						r.options.OnError(fmt.Errorf("refresh setup metadata: %w", err))
					}
				}()
			}
		case capturedAt := <-references.C:
			if r.motionIsActive() {
				continue
			}
			state, stateErr := r.client.BoardState(ctx)
			if stateErr != nil || !stableBoardState(state) || !boardStateMatchesWorld(state, tracker.current) {
				continue
			}
			world := tracker.snapshot(capturedAt.UTC(), WorldConfidenceTeacher)
			r.scheduleCapture(ctx, capturedAt.UTC(), "world-periodic", Label{
				LabelSource: "autodarts", Supervision: "teacher-world-state", CaptureReason: "periodic-world-observation",
				WorldAfter: worldPointer(world), Transition: &WorldTransition{Type: "world-observed", Source: "autodarts-state", Confidence: WorldConfidenceTeacher}, Context: CaptureContext{BoardState: state},
			})
		}
	}
}

func (r *Recorder) waitForStableBoardState(ctx context.Context, events <-chan autodarts.Event, failures <-chan error) (autodarts.BoardState, error) {
	const stableFor = 500 * time.Millisecond
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var candidate autodarts.BoardState
	var candidateSince time.Time
	for {
		state, err := r.client.BoardState(ctx)
		if err == nil && stableBoardState(state) && !r.motionIsActive() {
			if candidateSince.IsZero() || !sameBoardState(candidate, state) {
				candidate = state
				candidateSince = time.Now()
			} else if time.Since(candidateSince) >= stableFor {
				return state, nil
			}
		} else {
			candidateSince = time.Time{}
		}
		select {
		case <-ctx.Done():
			return autodarts.BoardState{}, ctx.Err()
		case event, ok := <-events:
			if !ok {
				return autodarts.BoardState{}, errors.New("board event stream closed while waiting for stable state")
			}
			r.rememberEvent(r.options.Now().UTC(), event)
			if event.Type == "motion_state" {
				candidateSince = time.Time{}
			}
		case failure, ok := <-failures:
			if ok && failure != nil {
				return autodarts.BoardState{}, failure
			}
		case <-ticker.C:
		}
	}
}

func stableBoardState(state autodarts.BoardState) bool {
	return state.Connected && state.Running && strings.EqualFold(strings.TrimSpace(state.Status), "throw")
}

func sameBoardState(left, right autodarts.BoardState) bool {
	if left.Connected != right.Connected || left.Running != right.Running || !strings.EqualFold(strings.TrimSpace(left.Status), strings.TrimSpace(right.Status)) || len(left.Throws) != len(right.Throws) {
		return false
	}
	for index := range left.Throws {
		if left.Throws[index] != right.Throws[index] {
			return false
		}
	}
	return true
}

func boardStateMatchesWorld(state autodarts.BoardState, world PhysicalBoardState) bool {
	if len(state.Throws) != len(world.Darts) {
		return false
	}
	for index := range state.Throws {
		if state.Throws[index].Coordinates != world.Darts[index].Coordinates || state.Throws[index].Segment != world.Darts[index].Segment {
			return false
		}
	}
	return true
}

func (r *Recorder) refreshSetup(ctx context.Context) error {
	metadataContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	snapshot := SetupSnapshot{
		SchemaVersion: SchemaVersion,
		SessionID:     r.sessionID,
		CapturedAt:    r.options.Now().UTC(),
	}
	var failures []error
	var failuresMu sync.Mutex
	recordFailure := func(name string, err error) {
		if err == nil {
			return
		}
		failuresMu.Lock()
		failures = append(failures, fmt.Errorf("%s: %w", name, err))
		failuresMu.Unlock()
	}

	var wait sync.WaitGroup
	run := func(name string, action func() error) {
		wait.Add(1)
		go func() {
			defer wait.Done()
			recordFailure(name, action())
		}()
	}
	run("version", func() error {
		value, err := r.client.Version(metadataContext)
		snapshot.BoardManagerVersion = value
		return err
	})
	run("host", func() error {
		value, err := r.client.Host(metadataContext)
		snapshot.Host = SetupHost{
			ClientVersion: value.ClientVersion, OS: value.OS, Platform: value.Platform,
			PlatformVersion: value.PlatformVersion, KernelArch: value.KernelArch, KernelVersion: value.KernelVersion,
		}
		return err
	})
	run("camera state", func() error {
		value, err := r.client.CameraState(metadataContext)
		snapshot.CameraState = value
		return err
	})
	run("camera stats", func() error {
		value, err := r.client.CameraStats(metadataContext)
		snapshot.CameraStats = value
		return err
	})
	run("detection stats", func() error {
		value, err := r.client.DetectionStats(metadataContext)
		snapshot.DetectionStats = value
		return err
	})
	run("camera resolution", func() error {
		value, err := r.client.CameraResolution(metadataContext)
		snapshot.CameraResolution = value
		return err
	})
	run("devices", func() error {
		values, err := r.client.Devices(metadataContext)
		if err != nil {
			return err
		}
		for _, value := range values {
			identity := fmt.Sprintf("%s\x00%s\x00%d\x00%d", value.Bus, value.Card, value.VendorID, value.ProductID)
			digest := sha256.Sum256([]byte(identity))
			snapshot.DeviceFingerprints = append(snapshot.DeviceFingerprints, "sha256:"+fmt.Sprintf("%x", digest))
		}
		sort.Strings(snapshot.DeviceFingerprints)
		return nil
	})
	run("config", func() error {
		value, err := r.client.Config(metadataContext)
		if err != nil {
			return err
		}
		snapshot.Config, err = sanitizedConfig(value)
		return err
	})
	run("calibration", func() error {
		value, err := r.client.Calibration(metadataContext)
		if err == nil {
			snapshot.Calibration, err = sanitizedJSON(value)
		}
		return err
	})
	run("distortion", func() error {
		value, err := r.client.Distortion(metadataContext)
		if err == nil {
			snapshot.Distortion, err = sanitizedJSON(value)
		}
		return err
	})
	run("calibration ellipses", func() error {
		value, err := r.client.CalibrationEllipses(metadataContext)
		if err == nil {
			snapshot.CalibrationEllipses, err = sanitizedJSON(value)
		}
		return err
	})
	wait.Wait()

	identity := struct {
		CameraResolution    autodarts.Resolution `json:"camera_resolution"`
		DeviceFingerprints  []string             `json:"device_fingerprints,omitempty"`
		Config              json.RawMessage      `json:"config,omitempty"`
		Calibration         json.RawMessage      `json:"calibration,omitempty"`
		Distortion          json.RawMessage      `json:"distortion,omitempty"`
		CalibrationEllipses json.RawMessage      `json:"calibration_ellipses,omitempty"`
	}{
		CameraResolution: snapshot.CameraResolution, DeviceFingerprints: snapshot.DeviceFingerprints,
		Config: snapshot.Config, Calibration: snapshot.Calibration, Distortion: snapshot.Distortion,
		CalibrationEllipses: snapshot.CalibrationEllipses,
	}
	encodedIdentity, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(encodedIdentity)
	snapshot.SetupID = fmt.Sprintf("%x", digest[:12])
	if err := r.writeSetupSnapshot(snapshot); err != nil {
		return err
	}
	r.mu.Lock()
	r.setup = snapshot
	r.mu.Unlock()
	return errors.Join(failures...)
}

func (r *Recorder) writeSetupSnapshot(snapshot SetupSnapshot) error {
	directory := filepath.Join(r.options.OutputDir, ".sessions", r.sessionID)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	path := filepath.Join(directory, snapshot.SetupID+".json")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(directory, ".setup-*.json")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

func sanitizedConfig(raw json.RawMessage) (json.RawMessage, error) {
	var source map[string]any
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, err
	}
	allowed := map[string]bool{
		"board_rois": true, "calibration": true, "cam": true, "cam_controls": true,
		"dartboard": true, "detection": true, "distortion": true, "motion": true,
	}
	result := make(map[string]any)
	for key, value := range source {
		if allowed[key] {
			result[key] = sanitizeValue(value)
		}
	}
	return json.Marshal(result)
}

func sanitizedJSON(raw json.RawMessage) (json.RawMessage, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return json.Marshal(sanitizeValue(value))
}

func sanitizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any)
		for key, child := range typed {
			if sensitiveMetadataKey(key) {
				continue
			}
			result[key] = sanitizeValue(child)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = sanitizeValue(child)
		}
		return result
	default:
		return value
	}
}

func sensitiveMetadataKey(key string) bool {
	normalized := strings.Map(func(character rune) rune {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			return character
		}
		if character >= 'A' && character <= 'Z' {
			return character + ('a' - 'A')
		}
		return -1
	}, key)
	switch normalized {
	case "auth", "apikey", "boardid", "cams", "hostname", "ip", "password", "secret", "tlscert", "tlskey", "token":
		return true
	default:
		return false
	}
}

func (r *Recorder) rememberEvent(receivedAt time.Time, event autodarts.Event) {
	recorded := RecordedEvent{ReceivedAt: receivedAt, Type: event.Type, Data: append(json.RawMessage(nil), event.Data...)}
	r.mu.Lock()
	defer r.mu.Unlock()
	if event.Type == "motion_state" {
		r.motionActive = inspectMotion(event.Data).Active
	}
	r.latestEvents[event.Type] = recorded
	r.recentEvents = append(r.recentEvents, recorded)
	cutoff := receivedAt.Add(-10 * time.Second)
	first := 0
	for first < len(r.recentEvents) && r.recentEvents[first].ReceivedAt.Before(cutoff) {
		first++
	}
	if first > 0 {
		r.recentEvents = append([]RecordedEvent(nil), r.recentEvents[first:]...)
	}
	if len(r.recentEvents) > 256 {
		r.recentEvents = append([]RecordedEvent(nil), r.recentEvents[len(r.recentEvents)-256:]...)
	}
}

func (r *Recorder) motionIsActive() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.motionActive
}

func (r *Recorder) stableCaptureAllowed(ctx context.Context, label Label, capturedAt time.Time) (bool, error) {
	state, err := r.client.BoardState(ctx)
	if err != nil {
		return false, err
	}
	if !stableBoardState(state) || label.WorldAfter == nil || !boardStateMatchesWorld(state, *label.WorldAfter) {
		return false, nil
	}
	windowStart := capturedAt.Add(r.options.BurstOffsets[0])
	windowEnd := capturedAt.Add(r.options.BurstOffsets[len(r.options.BurstOffsets)-1])
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.motionActive {
		return false, nil
	}
	for _, event := range r.recentEvents {
		if event.Type == "motion_state" && !event.ReceivedAt.Before(windowStart) && !event.ReceivedAt.After(windowEnd) && inspectMotion(event.Data).Active {
			return false, nil
		}
	}
	return true, nil
}

func (r *Recorder) captureMetadata(boardState autodarts.BoardState, capturedAt time.Time) (SetupSnapshot, CaptureContext) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	setup := r.setup
	context := CaptureContext{BoardState: boardState, LatestEvents: make(map[string]RecordedEvent, len(r.latestEvents))}
	for name, event := range r.latestEvents {
		context.LatestEvents[name] = cloneRecordedEvent(event)
	}
	cutoff := capturedAt.Add(-5 * time.Second)
	for _, event := range r.recentEvents {
		if !event.ReceivedAt.Before(cutoff) {
			context.RecentEvents = append(context.RecentEvents, cloneRecordedEvent(event))
		}
	}
	if boardStateIsEmpty(boardState) {
		if event, ok := context.LatestEvents["state"]; ok {
			_ = json.Unmarshal(event.Data, &context.BoardState)
		}
	}
	return setup, context
}

func boardStateIsEmpty(state autodarts.BoardState) bool {
	return !state.Connected && !state.Running && state.Status == "" && state.Event == "" && state.NumThrows == 0 && len(state.Throws) == 0
}

func cloneRecordedEvent(event RecordedEvent) RecordedEvent {
	event.Data = append(json.RawMessage(nil), event.Data...)
	return event
}

func (r *Recorder) readCamera(ctx context.Context, camera int) {
	endpoint := fmt.Sprintf("%s/api/streams/cams/%d", strings.TrimRight(r.options.BoardURL, "/"), camera)
	for ctx.Err() == nil {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		res, err := r.options.Client.Do(req)
		if err == nil && res.StatusCode == http.StatusOK {
			mediaType, params, parseErr := mime.ParseMediaType(res.Header.Get("Content-Type"))
			if parseErr == nil && strings.HasPrefix(mediaType, "multipart/") && params["boundary"] != "" {
				reader := multipart.NewReader(res.Body, params["boundary"])
				for ctx.Err() == nil {
					part, partErr := reader.NextPart()
					if partErr != nil {
						break
					}
					data, readErr := io.ReadAll(io.LimitReader(part, 4<<20))
					part.Close()
					if readErr == nil && len(data) > 2 && data[0] == 0xff && data[1] == 0xd8 {
						r.buffers[camera].add(frame{at: r.options.Now().UTC(), jpeg: data})
					}
				}
			}
			res.Body.Close()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (r *Recorder) scheduleCapture(ctx context.Context, capturedAt time.Time, suffix string, label Label) {
	r.captureWG.Add(1)
	go func() {
		defer r.captureWG.Done()
		if err := r.capture(ctx, capturedAt, suffix, label); err != nil && r.options.OnError != nil {
			r.options.OnError(err)
		}
	}()
}

func (r *Recorder) capture(ctx context.Context, capturedAt time.Time, suffix string, label Label) error {
	maxOffset := r.options.BurstOffsets[len(r.options.BurstOffsets)-1]
	if maxOffset < 0 {
		maxOffset = 0
	}
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(maxOffset):
	}
	if label.CaptureReason == "startup-world-observation" || label.CaptureReason == "periodic-world-observation" {
		allowed, err := r.stableCaptureAllowed(ctx, label, capturedAt)
		if err != nil {
			return fmt.Errorf("validate stable world observation: %w", err)
		}
		if !allowed {
			return errors.New("stable world observation invalidated by board state or motion")
		}
	}
	type selectedFrame struct {
		camera int
		offset time.Duration
		frame  frame
		digest [sha256.Size]byte
	}
	var selected []selectedFrame
	uniqueFrames := make([]map[[sha256.Size]byte]bool, len(r.buffers))
	for camera := range r.buffers {
		uniqueFrames[camera] = make(map[[sha256.Size]byte]bool)
		for _, offset := range r.options.BurstOffsets {
			target := capturedAt.Add(offset)
			value, ok := r.buffers[camera].closest(target)
			if !ok {
				return fmt.Errorf("camera %d has no frame near %s", camera, offset)
			}
			if distance := absDuration(value.at.Sub(target)); distance > maxFrameSkew {
				return fmt.Errorf("camera %d nearest frame for %s is stale by %s", camera, offset, distance)
			}
			digest := sha256.Sum256(value.jpeg)
			uniqueFrames[camera][digest] = true
			selected = append(selected, selectedFrame{camera: camera, offset: offset, frame: value, digest: digest})
		}
		if len(uniqueFrames[camera]) < 2 {
			return fmt.Errorf("camera %d capture window contains only one unique frame", camera)
		}
	}
	sampleID := fmt.Sprintf("%s-%s", capturedAt.Format("20060102T150405.000000000Z"), suffix)
	temporary, err := os.MkdirTemp(r.options.OutputDir, ".sample-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	files := FrameFiles{Before: make([]string, len(r.buffers)), After: make([]string, len(r.buffers))}
	sequence := make([]CapturedFrame, 0, len(selected))
	for index, item := range selected {
		name := fmt.Sprintf("cam-%d-frame-%02d.jpg", item.camera, index%len(r.options.BurstOffsets))
		if err := os.WriteFile(filepath.Join(temporary, name), item.frame.jpeg, 0600); err != nil {
			return err
		}
		sequence = append(sequence, CapturedFrame{
			Camera: item.camera, File: name, RequestedOffsetMS: item.offset.Milliseconds(), CapturedAt: item.frame.at,
			ActualOffsetMS: item.frame.at.Sub(capturedAt).Milliseconds(), SHA256: fmt.Sprintf("%x", item.digest),
			Role:       frameRole(label.CaptureReason, item.offset, r.options.PreDelay, r.options.PostDelay),
			WorldState: frameWorldState(label.CaptureReason, item.offset, r.options.PreDelay, r.options.PostDelay),
		})
		if item.offset == -r.options.PreDelay {
			files.Before[item.camera] = name
		}
		if item.offset == r.options.PostDelay {
			files.After[item.camera] = name
		}
	}
	setup, context := r.captureMetadata(label.Context.BoardState, capturedAt)
	label.SchemaVersion = SchemaVersion
	label.SampleID = sampleID
	label.SessionID = r.sessionID
	label.SetupID = setup.SetupID
	label.SetupFile = filepath.ToSlash(filepath.Join(".sessions", r.sessionID, setup.SetupID+".json"))
	label.CapturedAt = capturedAt
	label.Frames = files
	label.FrameSequence = sequence
	label.Context = context
	if label.ReviewStatus == "" {
		label.ReviewStatus = "unreviewed"
	}
	encoded, err := json.MarshalIndent(label, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(filepath.Join(temporary, "label.json"), encoded, 0600); err != nil {
		return err
	}
	path := filepath.Join(r.options.OutputDir, sampleID)
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	if r.options.Catalog != nil {
		if err := r.options.Catalog.AddPath(label, path); err != nil {
			return err
		}
		err = r.options.Catalog.EnforceQuota()
	} else {
		r.quotaMu.Lock()
		err = r.enforceQuota()
		r.quotaMu.Unlock()
	}
	if err != nil {
		return err
	}
	if r.options.OnSample != nil {
		r.options.OnSample(path, label)
	}
	return nil
}

func frameRole(reason string, offset, preDelay, postDelay time.Duration) string {
	switch reason {
	case "startup-world-observation", "periodic-world-observation":
		return "stable-world"
	case "teacher-dart-added", "teacher-world-reconciled":
		switch {
		case offset <= -preDelay:
			return "stable-before"
		case offset >= postDelay:
			return "stable-after"
		default:
			return "transition"
		}
	case "motion-started":
		if offset <= -preDelay {
			return "stable-before"
		}
		return "transition"
	case "motion-settled":
		if offset >= postDelay {
			return "stable-unlabeled-after"
		}
		return "transition"
	default:
		return "observation"
	}
}

func frameWorldState(reason string, offset, preDelay, postDelay time.Duration) string {
	switch reason {
	case "startup-world-observation", "periodic-world-observation":
		return "after"
	case "teacher-dart-added", "teacher-world-reconciled":
		if offset <= -preDelay {
			return "before"
		}
		if offset >= postDelay {
			return "after"
		}
	case "motion-started":
		if offset <= -preDelay {
			return "before"
		}
	}
	return ""
}

func (r *Recorder) enforceQuota() error {
	entries, err := os.ReadDir(r.options.OutputDir)
	if err != nil {
		return err
	}
	type sample struct {
		name string
		size int64
	}
	var samples []sample
	var total int64
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		var size int64
		filepath.WalkDir(filepath.Join(r.options.OutputDir, entry.Name()), func(_ string, item os.DirEntry, walkErr error) error {
			if walkErr == nil && !item.IsDir() {
				if info, infoErr := item.Info(); infoErr == nil {
					size += info.Size()
				}
			}
			return nil
		})
		samples = append(samples, sample{name: entry.Name(), size: size})
		total += size
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].name < samples[j].name })
	for _, sample := range samples {
		if total <= r.options.Quota {
			break
		}
		if err := os.RemoveAll(filepath.Join(r.options.OutputDir, sample.name)); err != nil {
			return err
		}
		total -= sample.size
	}
	return nil
}
