package dataset

import (
	"context"
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

const SchemaVersion = 1

type Label struct {
	SchemaVersion int                   `json:"schema_version"`
	SampleID      string                `json:"sample_id"`
	CapturedAt    time.Time             `json:"captured_at"`
	LabelSource   string                `json:"label_source"`
	Coordinates   autodarts.Coordinates `json:"coordinates"`
	Segment       autodarts.Segment     `json:"segment"`
	DartIndex     int                   `json:"dart_index"`
	DartCount     int                   `json:"dart_count"`
	Frames        FrameFiles            `json:"frames"`
	ReviewStatus  string                `json:"review_status"`
}

type FrameFiles struct {
	Before []string `json:"before"`
	After  []string `json:"after"`
}

type Options struct {
	BoardURL  string
	OutputDir string
	Cameras   int
	PreDelay  time.Duration
	PostDelay time.Duration
	Quota     int64
	Client    *http.Client
	Now       func() time.Time
	OnSample  func(path string, label Label)
	OnError   func(error)
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

func (b *cameraBuffer) closest(target time.Time) ([]byte, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.frames) == 0 {
		return nil, false
	}
	best := b.frames[0]
	bestDistance := absDuration(best.at.Sub(target))
	for _, candidate := range b.frames[1:] {
		distance := absDuration(candidate.at.Sub(target))
		if distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	return append([]byte(nil), best.jpeg...), true
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

type Recorder struct {
	options Options
	client  *autodarts.Client
	buffers []cameraBuffer
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
	return &Recorder{options: options, client: client, buffers: make([]cameraBuffer, options.Cameras)}, nil
}

func (r *Recorder) Run(ctx context.Context) error {
	if err := os.MkdirAll(r.options.OutputDir, 0700); err != nil {
		return err
	}
	for camera := range r.buffers {
		go r.readCamera(ctx, camera)
	}
	initial, err := r.client.BoardState(ctx)
	if err != nil {
		return fmt.Errorf("read initial board state: %w", err)
	}
	knownThrows := len(initial.Throws)
	events, failures, err := r.client.Events(ctx)
	if err != nil {
		return fmt.Errorf("subscribe to board events: %w", err)
	}
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
			if event.Type != "state" {
				continue
			}
			var state autodarts.BoardState
			if err := json.Unmarshal(event.Data, &state); err != nil {
				continue
			}
			if len(state.Throws) < knownThrows {
				knownThrows = len(state.Throws)
			}
			for index := knownThrows; index < len(state.Throws); index++ {
				capturedAt := r.options.Now().UTC()
				if err := r.capture(ctx, capturedAt, index, state); err != nil {
					if r.options.OnError != nil {
						r.options.OnError(err)
					}
				}
			}
			knownThrows = len(state.Throws)
		}
	}
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

func (r *Recorder) capture(ctx context.Context, capturedAt time.Time, index int, state autodarts.BoardState) error {
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(r.options.PostDelay):
	}
	before, after := make([][]byte, len(r.buffers)), make([][]byte, len(r.buffers))
	for camera := range r.buffers {
		var ok bool
		before[camera], ok = r.buffers[camera].closest(capturedAt.Add(-r.options.PreDelay))
		if !ok {
			return fmt.Errorf("camera %d has no buffered frame", camera)
		}
		after[camera], ok = r.buffers[camera].closest(capturedAt.Add(r.options.PostDelay))
		if !ok {
			return fmt.Errorf("camera %d has no post-dart frame", camera)
		}
	}
	sampleID := fmt.Sprintf("%s-dart-%d", capturedAt.Format("20060102T150405.000000000Z"), index+1)
	temporary, err := os.MkdirTemp(r.options.OutputDir, ".sample-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	files := FrameFiles{Before: make([]string, len(before)), After: make([]string, len(after))}
	for camera := range before {
		files.Before[camera] = fmt.Sprintf("cam-%d-before.jpg", camera)
		files.After[camera] = fmt.Sprintf("cam-%d-after.jpg", camera)
		if err := os.WriteFile(filepath.Join(temporary, files.Before[camera]), before[camera], 0600); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(temporary, files.After[camera]), after[camera], 0600); err != nil {
			return err
		}
	}
	dart := state.Throws[index]
	label := Label{SchemaVersion: SchemaVersion, SampleID: sampleID, CapturedAt: capturedAt, LabelSource: "autodarts", Coordinates: dart.Coordinates, Segment: dart.Segment, DartIndex: index + 1, DartCount: len(state.Throws), Frames: files, ReviewStatus: "unreviewed"}
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
	if err := r.enforceQuota(); err != nil {
		return err
	}
	if r.options.OnSample != nil {
		r.options.OnSample(path, label)
	}
	return nil
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
