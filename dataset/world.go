package dataset

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jonaswre/autodarts-nixos/autodarts"
)

const (
	WorldConfidenceTeacher        = "teacher"
	WorldConfidenceReviewRequired = "teacher-review-required"
	WorldConfidenceUnlabeled      = "unlabeled"
)

// PhysicalBoardState is a game-independent observation of the darts believed
// to be physically present on the board. Darts is deliberately unbounded by a
// three-dart visit.
type PhysicalBoardState struct {
	StateID    string         `json:"state_id"`
	ObservedAt time.Time      `json:"observed_at"`
	Source     string         `json:"source"`
	Confidence string         `json:"confidence"`
	Darts      []PhysicalDart `json:"darts"`
}

type PhysicalDart struct {
	ID          string                `json:"id"`
	Order       int                   `json:"order"`
	Coordinates autodarts.Coordinates `json:"coordinates"`
	Segment     autodarts.Segment     `json:"segment"`
}

// WorldTransition describes observable physical change only. It intentionally
// contains no player, visit, score, or game-state concepts.
type WorldTransition struct {
	Type         string         `json:"type"`
	Source       string         `json:"source"`
	Confidence   string         `json:"confidence"`
	Added        []PhysicalDart `json:"added,omitempty"`
	Removed      []PhysicalDart `json:"removed,omitempty"`
	Updated      []PhysicalDart `json:"updated,omitempty"`
	StillPresent []PhysicalDart `json:"still_present,omitempty"`
	Uncertain    []PhysicalDart `json:"uncertain,omitempty"`
	Evidence     []string       `json:"evidence,omitempty"`
}

type worldTracker struct {
	sessionID    string
	stateCounter int
	dartCounter  int
	current      PhysicalBoardState
}

func newWorldTracker(sessionID string, state autodarts.BoardState, observedAt time.Time) *worldTracker {
	tracker := &worldTracker{sessionID: sessionID}
	tracker.current = tracker.initialState(state, observedAt)
	return tracker
}

func (t *worldTracker) initialState(state autodarts.BoardState, observedAt time.Time) PhysicalBoardState {
	darts := make([]PhysicalDart, len(state.Throws))
	for index, dart := range state.Throws {
		darts[index] = t.newDart(index+1, dart)
	}
	return t.newState(observedAt, WorldConfidenceTeacher, darts)
}

func (t *worldTracker) observe(state autodarts.BoardState, observedAt time.Time, confidence string) (PhysicalBoardState, PhysicalBoardState, WorldTransition, bool) {
	before := clonePhysicalBoardState(t.current)
	afterDarts := t.reconcileDarts(before.Darts, state.Throws)
	after := PhysicalBoardState{
		StateID: before.StateID, ObservedAt: observedAt, Source: "autodarts-state",
		Confidence: confidence, Darts: clonePhysicalDarts(afterDarts),
	}
	transition := transitionBetween(before, after, "autodarts-state", confidence)
	changed := transition.Type != "unchanged"
	if changed {
		after = t.newState(observedAt, confidence, afterDarts)
		t.current = after
	} else {
		// Retain the stable state identity while refreshing its observation time
		// and confidence for later periodic captures.
		t.current.ObservedAt = observedAt
		t.current.Confidence = confidence
		after = clonePhysicalBoardState(t.current)
	}
	return before, after, transition, changed
}

func (t *worldTracker) snapshot(observedAt time.Time, confidence string) PhysicalBoardState {
	value := clonePhysicalBoardState(t.current)
	value.ObservedAt = observedAt
	value.Confidence = confidence
	return value
}

func (t *worldTracker) reconcileDarts(previous []PhysicalDart, observed []autodarts.Throw) []PhysicalDart {
	result := make([]PhysicalDart, len(observed))
	type candidateMatch struct {
		previous int
		observed int
		distance float64
	}
	var candidates []candidateMatch
	for observedIndex, dart := range observed {
		for previousIndex, candidate := range previous {
			distance := coordinateDistance(candidate.Coordinates, dart.Coordinates)
			if distance <= 0.15 {
				candidates = append(candidates, candidateMatch{previous: previousIndex, observed: observedIndex, distance: distance})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].distance < candidates[j].distance })
	usedPrevious := make([]bool, len(previous))
	usedObserved := make([]bool, len(observed))
	for _, candidate := range candidates {
		if usedPrevious[candidate.previous] || usedObserved[candidate.observed] {
			continue
		}
		usedPrevious[candidate.previous] = true
		usedObserved[candidate.observed] = true
		dart := observed[candidate.observed]
		result[candidate.observed] = previous[candidate.previous]
		result[candidate.observed].Order = candidate.observed + 1
		result[candidate.observed].Coordinates = dart.Coordinates
		result[candidate.observed].Segment = dart.Segment
	}
	for index, dart := range observed {
		if !usedObserved[index] {
			result[index] = t.newDart(index+1, dart)
		}
	}
	return result
}

func (t *worldTracker) newDart(order int, dart autodarts.Throw) PhysicalDart {
	t.dartCounter++
	return PhysicalDart{
		ID: fmt.Sprintf("%s-dart-%06d", t.sessionID, t.dartCounter), Order: order,
		Coordinates: dart.Coordinates, Segment: dart.Segment,
	}
}

func (t *worldTracker) newState(observedAt time.Time, confidence string, darts []PhysicalDart) PhysicalBoardState {
	t.stateCounter++
	return PhysicalBoardState{
		StateID: fmt.Sprintf("%s-world-%06d", t.sessionID, t.stateCounter), ObservedAt: observedAt,
		Source: "autodarts-state", Confidence: confidence, Darts: clonePhysicalDarts(darts),
	}
}

func transitionBetween(before, after PhysicalBoardState, source, confidence string) WorldTransition {
	transition := WorldTransition{Type: "unchanged", Source: source, Confidence: confidence}
	beforeByID, afterByID := make(map[string]PhysicalDart), make(map[string]PhysicalDart)
	for _, dart := range before.Darts {
		beforeByID[dart.ID] = dart
	}
	for _, dart := range after.Darts {
		afterByID[dart.ID] = dart
	}
	for _, dart := range after.Darts {
		previous, exists := beforeByID[dart.ID]
		if !exists {
			transition.Added = append(transition.Added, dart)
		} else {
			transition.StillPresent = append(transition.StillPresent, dart)
			if previous.Coordinates != dart.Coordinates || previous.Segment != dart.Segment {
				transition.Updated = append(transition.Updated, dart)
			}
		}
	}
	for _, dart := range before.Darts {
		if _, exists := afterByID[dart.ID]; !exists {
			transition.Removed = append(transition.Removed, dart)
		}
	}
	switch {
	case len(transition.Added) > 0 && len(transition.Removed) == 0 && len(transition.Updated) == 0:
		transition.Type = "darts-added"
	case len(transition.Removed) > 0 && len(transition.Added) == 0 && len(transition.Updated) == 0:
		transition.Type = "darts-removed"
	case len(transition.Added)+len(transition.Removed)+len(transition.Updated) > 0:
		transition.Type = "world-reconciled"
	}
	if confidence == WorldConfidenceReviewRequired {
		transition.Uncertain = clonePhysicalDarts(after.Darts)
	}
	return transition
}

func clonePhysicalBoardState(value PhysicalBoardState) PhysicalBoardState {
	value.Darts = clonePhysicalDarts(value.Darts)
	return value
}

func clonePhysicalDarts(values []PhysicalDart) []PhysicalDart {
	result := make([]PhysicalDart, len(values))
	copy(result, values)
	return result
}

func worldPointer(value PhysicalBoardState) *PhysicalBoardState {
	copy := clonePhysicalBoardState(value)
	return &copy
}

func coordinateDistance(left, right autodarts.Coordinates) float64 {
	return math.Hypot(left.X-right.X, left.Y-right.Y)
}

type motionEvidence struct {
	Active bool
	Hints  []string
}

func inspectMotion(raw []byte) motionEvidence {
	type cameraMotion struct {
		IsDart    bool  `json:"isDart"`
		IsHand    bool  `json:"isHand"`
		IsStable  *bool `json:"isStable"`
		IsTakeout bool  `json:"isTakeout"`
	}
	var value struct {
		IsDart           bool  `json:"isDart"`
		IsHand           bool  `json:"isHand"`
		IsStable         *bool `json:"isStable"`
		IsTakeoutFull    bool  `json:"isTakeoutFull"`
		IsTakeoutPartial bool  `json:"isTakeoutPartial"`
		IsWaiting        bool  `json:"isWaiting"`
		FrameFlags       struct {
			Dart    bool `json:"dart"`
			Hand    bool `json:"hand"`
			Takeout bool `json:"takeout"`
		} `json:"frameFlags"`
		FrameCounts struct {
			Dart    int `json:"dart"`
			Hand    int `json:"hand"`
			Takeout int `json:"takeout"`
			Wait    int `json:"wait"`
		} `json:"frameCounts"`
		CamStates []cameraMotion `json:"camStates"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return motionEvidence{}
	}
	hints := map[string]bool{}
	add := func(condition bool, value string) {
		if condition {
			hints[value] = true
		}
	}
	add(value.IsDart || value.FrameFlags.Dart || value.FrameCounts.Dart > 0, "board-manager-dart-signal")
	add(value.IsHand || value.FrameFlags.Hand || value.FrameCounts.Hand > 0, "board-manager-hand-signal")
	add(value.IsTakeoutPartial, "board-manager-partial-takeout-signal")
	add(value.IsTakeoutFull, "board-manager-full-takeout-signal")
	add(value.FrameFlags.Takeout || value.FrameCounts.Takeout > 0, "board-manager-takeout-signal")
	add(value.IsWaiting || value.FrameCounts.Wait > 0, "board-manager-wait-signal")
	add(value.IsStable != nil && !*value.IsStable, "board-manager-unstable-signal")
	for _, camera := range value.CamStates {
		add(camera.IsDart, "board-manager-dart-signal")
		add(camera.IsHand, "board-manager-hand-signal")
		add(camera.IsTakeout, "board-manager-takeout-signal")
		add(camera.IsStable != nil && !*camera.IsStable, "board-manager-unstable-signal")
	}
	result := motionEvidence{Active: len(hints) > 0}
	for hint := range hints {
		result.Hints = append(result.Hints, hint)
	}
	sort.Strings(result.Hints)
	return result
}

func mergeEvidence(existing, additional []string) []string {
	values := make(map[string]bool, len(existing)+len(additional))
	for _, value := range existing {
		values[value] = true
	}
	for _, value := range additional {
		values[value] = true
	}
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsTakeoutEvidence(values []string) bool {
	for _, value := range values {
		if value == "board-manager-partial-takeout-signal" || value == "board-manager-full-takeout-signal" || value == "board-manager-takeout-signal" {
			return true
		}
	}
	return false
}
