package dataset

import (
	"testing"
	"time"

	"github.com/jonaswre/autodarts-nixos/autodarts"
)

func TestWorldTrackerSupportsUnboundedDartSetsAndPersistentIdentity(t *testing.T) {
	observedAt := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	tracker := newWorldTracker("session-test", autodarts.BoardState{}, observedAt)
	fourDarts := []autodarts.Throw{
		testThrow(0.10, 0.20, "S1"),
		testThrow(0.30, 0.40, "D2"),
		testThrow(-0.20, 0.15, "T3"),
		testThrow(-0.35, -0.25, "S4"),
	}

	before, four, added, changed := tracker.observe(autodarts.BoardState{Throws: fourDarts}, observedAt.Add(time.Second), WorldConfidenceTeacher)
	if !changed || len(before.Darts) != 0 || len(four.Darts) != 4 || len(added.Added) != 4 {
		t.Fatalf("four-dart physical state was truncated: before=%+v after=%+v transition=%+v", before, four, added)
	}
	originalIDs := []string{four.Darts[0].ID, four.Darts[1].ID, four.Darts[2].ID, four.Darts[3].ID}

	remainingThrows := []autodarts.Throw{fourDarts[0], fourDarts[2], fourDarts[3]}
	_, remaining, removed, changed := tracker.observe(autodarts.BoardState{Throws: remainingThrows}, observedAt.Add(2*time.Second), WorldConfidenceTeacher)
	if !changed || len(remaining.Darts) != 3 || len(removed.Removed) != 1 || len(removed.StillPresent) != 3 || len(removed.Updated) != 0 || removed.Type != "darts-removed" {
		t.Fatalf("partial removal transition is incomplete: after=%+v transition=%+v", remaining, removed)
	}
	if removed.Removed[0].ID != originalIDs[1] || remaining.Darts[0].ID != originalIDs[0] || remaining.Darts[1].ID != originalIDs[2] || remaining.Darts[2].ID != originalIDs[3] {
		t.Fatalf("physical dart identities were not preserved: original=%v remaining=%+v removed=%+v", originalIDs, remaining.Darts, removed.Removed)
	}

	withNewDart := append(append([]autodarts.Throw{}, remainingThrows...), testThrow(0.45, -0.10, "D5"))
	_, afterAdd, next, changed := tracker.observe(autodarts.BoardState{Throws: withNewDart}, observedAt.Add(3*time.Second), WorldConfidenceTeacher)
	if !changed || len(afterAdd.Darts) != 4 || len(next.Added) != 1 || len(next.StillPresent) != 3 || len(next.Removed) != 0 {
		t.Fatalf("new dart after partial removal was not represented correctly: after=%+v transition=%+v", afterAdd, next)
	}
	if next.Added[0].ID == "" || next.Added[0].ID == originalIDs[1] {
		t.Fatalf("new physical dart did not receive a new identity: %+v", next.Added[0])
	}
}

func TestMotionEvidenceRemainsRawAndUnclassified(t *testing.T) {
	evidence := inspectMotion([]byte(`{
  "isStable": false,
  "isTakeoutPartial": true,
  "frameFlags": {"dart": true, "hand": true}
}`))
	if !evidence.Active || !containsTakeoutEvidence(evidence.Hints) {
		t.Fatalf("motion evidence was not retained: %+v", evidence)
	}
	for _, hint := range evidence.Hints {
		if hint == "miss" || hint == "close-miss" || hint == "throw" {
			t.Fatalf("recorder inferred a classification from raw evidence: %+v", evidence.Hints)
		}
	}
}

func testThrow(x, y float64, name string) autodarts.Throw {
	return autodarts.Throw{Coordinates: autodarts.Coordinates{X: x, Y: y}, Segment: autodarts.Segment{Name: name}}
}
