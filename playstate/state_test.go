package playstate

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"
)

func TestRandomCheckoutJourneyProvidesLiveLEDTarget(t *testing.T) {
	file, err := os.Open(filepath.Join("..", "protocol", "testdata", "play", "random-checkout-64.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var state Snapshot
	seen := map[string]bool{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		changed, err := state.Apply(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if !changed {
			continue
		}
		if target, ok := state.NextTarget(); ok {
			seen[target.Name] = true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{"T16", "S16", "S5", "S3", "S15", "D2"} {
		if !seen[target] {
			t.Errorf("journey never exposed target %s", target)
		}
	}
	if state.Variant != "Random Checkout" || !state.Finished || state.Remaining != 0 || state.Winner != 0 {
		t.Fatalf("unexpected final state: %+v", state)
	}
	if _, ok := state.NextTarget(); ok {
		t.Error("finished match still exposes an LED target")
	}
	if state.LastThrow == nil || state.LastThrow.Name != "D2" {
		t.Fatalf("final detected throw = %+v, want D2", state.LastThrow)
	}
}
