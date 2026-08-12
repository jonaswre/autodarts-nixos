package autodarts

import "encoding/json"

type Segment struct {
	Name       string `json:"name"`
	Number     int    `json:"number"`
	Bed        string `json:"bed"`
	Multiplier int    `json:"multiplier"`
}

type Coordinates struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Throw struct {
	Coordinates Coordinates `json:"coords"`
	Segment     Segment     `json:"segment"`
}

type BoardState struct {
	Connected bool    `json:"connected"`
	Running   bool    `json:"running"`
	Status    string  `json:"status"`
	Event     string  `json:"event"`
	NumThrows int     `json:"numThrows"`
	Throws    []Throw `json:"throws,omitempty"`
}

type Resolution struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}
type CameraState struct {
	IsOpened  bool `json:"isOpened"`
	IsRunning bool `json:"isRunning"`
}
type CameraStats struct {
	FPS        []float64  `json:"fps"`
	Resolution Resolution `json:"resolution"`
}
type DetectionStats struct {
	FPS        float64    `json:"fps"`
	Resolution Resolution `json:"resolution"`
}

type Host struct {
	ClientVersion   string `json:"clientVersion"`
	Hostname        string `json:"hostname"`
	IP              string `json:"ip"`
	OS              string `json:"os"`
	Platform        string `json:"platform"`
	PlatformVersion string `json:"platformVersion"`
	KernelArch      string `json:"kernelArch"`
	KernelVersion   string `json:"kernelVersion"`
}

type Device struct {
	Bus       string          `json:"bus"`
	Card      string          `json:"card"`
	ProductID int             `json:"productId"`
	VendorID  int             `json:"vendorId"`
	Formats   json.RawMessage `json:"formats"`
}

type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type Snapshot struct {
	Board         BoardState
	Variant       string
	Remaining     int
	Round         int
	MatchFinished bool
	Winner        int
	CheckoutGuide []Segment
	LastThrow     *Segment
}

func (s Snapshot) NextTarget() (Segment, bool) {
	if s.MatchFinished || len(s.CheckoutGuide) == 0 {
		return Segment{}, false
	}
	return s.CheckoutGuide[0], true
}
