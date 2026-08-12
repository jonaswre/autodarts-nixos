// Package playstate turns the Play web client's captured protocol into a small,
// stable view suitable for appliance features such as target LEDs.
package playstate

import "encoding/json"

type Segment struct {
	Name       string `json:"name"`
	Number     int    `json:"number"`
	Bed        string `json:"bed"`
	Multiplier int    `json:"multiplier"`
}

type Snapshot struct {
	Variant       string
	Remaining     int
	Round         int
	Finished      bool
	Winner        int
	CheckoutGuide []Segment
	LastThrow     *Segment
}

type capture struct {
	Message json.RawMessage `json:"message"`
}

type wireMessage struct {
	Channel  string          `json:"channel"`
	Data     json.RawMessage `json:"data"`
	Variant  string          `json:"variant"`
	Round    int             `json:"round"`
	Finished bool            `json:"gameFinished"`
	Winner   int             `json:"gameWinner"`
	Scores   []int           `json:"gameScores"`
	State    struct {
		CheckoutGuide []Segment `json:"checkoutGuide"`
	} `json:"state"`
}

// Apply consumes one JSONL capture record. It returns true only when that
// record changes user-visible match state.
func (s *Snapshot) Apply(line []byte) (bool, error) {
	var envelope capture
	if err := json.Unmarshal(line, &envelope); err != nil {
		return false, err
	}
	if len(envelope.Message) == 0 || envelope.Message[0] != '{' {
		return false, nil
	}
	var message wireMessage
	if err := json.Unmarshal(envelope.Message, &message); err != nil {
		return false, err
	}
	if message.Channel == "autodarts.matches" {
		var event struct {
			Event string `json:"event"`
			Body  struct {
				Segment Segment `json:"segment"`
			} `json:"body"`
		}
		if err := json.Unmarshal(message.Data, &event); err == nil && event.Event == "throw" {
			s.LastThrow = &event.Body.Segment
			return true, nil
		}
		var state wireMessage
		if err := json.Unmarshal(message.Data, &state); err != nil {
			return false, err
		}
		return s.applyState(state), nil
	}
	if message.Variant != "" {
		return s.applyState(message), nil
	}
	return false, nil
}

func (s *Snapshot) applyState(state wireMessage) bool {
	if state.Variant == "" {
		return false
	}
	s.Variant, s.Round, s.Finished, s.Winner = state.Variant, state.Round, state.Finished, state.Winner
	if len(state.Scores) > 0 {
		s.Remaining = state.Scores[0]
	}
	s.CheckoutGuide = append(s.CheckoutGuide[:0], state.State.CheckoutGuide...)
	return true
}

func (s Snapshot) NextTarget() (Segment, bool) {
	if s.Finished || len(s.CheckoutGuide) == 0 {
		return Segment{}, false
	}
	return s.CheckoutGuide[0], true
}
