package autodarts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jonaswre/autodarts-nixos/playstate"
)

type Client struct {
	mu     sync.RWMutex
	base   *url.URL
	http   *http.Client
	dialer *websocket.Dialer
	state  Snapshot
	play   playstate.Snapshot
}

func New(boardManagerURL string, options ...Option) (*Client, error) {
	base, err := url.Parse(strings.TrimRight(boardManagerURL, "/"))
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, fmt.Errorf("invalid Board Manager URL %q", boardManagerURL)
	}
	c := &Client{base: base, http: &http.Client{Timeout: 10 * time.Second}, dialer: websocket.DefaultDialer}
	for _, option := range options {
		option(c)
	}
	return c, nil
}

type Option func(*Client)

func WithHTTPClient(client *http.Client) Option { return func(c *Client) { c.http = client } }
func WithWebSocketDialer(dialer *websocket.Dialer) Option {
	return func(c *Client) { c.dialer = dialer }
}

func (c *Client) get(ctx context.Context, path string, target any) error {
	return c.request(ctx, http.MethodGet, path, nil, target)
}
func (c *Client) action(ctx context.Context, method, path string, body any) error {
	return c.request(ctx, method, path, body, nil)
}
func (c *Client) request(ctx context.Context, method, path string, body, target any) error {
	var input io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base.String()+"/api/"+path, input)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("%s api/%s: %s: %s", method, path, res.Status, strings.TrimSpace(string(data)))
	}
	if target == nil {
		io.Copy(io.Discard, res.Body)
		return nil
	}
	return json.NewDecoder(res.Body).Decode(target)
}

func (c *Client) BoardState(ctx context.Context) (BoardState, error) {
	var v BoardState
	err := c.get(ctx, "state", &v)
	return v, err
}
func (c *Client) Host(ctx context.Context) (Host, error) {
	var v Host
	err := c.get(ctx, "host", &v)
	return v, err
}
func (c *Client) Version(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base.String()+"/api/version", nil)
	if err != nil {
		return "", err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		return "", err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("GET api/version: %s: %s", res.Status, strings.TrimSpace(string(data)))
	}
	var quoted string
	if json.Unmarshal(data, &quoted) == nil {
		return quoted, nil
	}
	plain := strings.TrimSpace(string(data))
	if plain == "" {
		return "", errors.New("GET api/version: empty response")
	}
	return plain, nil
}
func (c *Client) CameraState(ctx context.Context) (CameraState, error) {
	var v CameraState
	err := c.get(ctx, "cams/state", &v)
	return v, err
}
func (c *Client) CameraStats(ctx context.Context) (CameraStats, error) {
	var v CameraStats
	err := c.get(ctx, "cams/stats", &v)
	return v, err
}
func (c *Client) DetectionStats(ctx context.Context) (DetectionStats, error) {
	var v DetectionStats
	err := c.get(ctx, "state/stats", &v)
	return v, err
}
func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	var v []Device
	err := c.get(ctx, "devices", &v)
	return v, err
}
func (c *Client) Config(ctx context.Context) (json.RawMessage, error) {
	var v json.RawMessage
	err := c.get(ctx, "config", &v)
	return v, err
}
func (c *Client) Calibration(ctx context.Context) (json.RawMessage, error) {
	var v json.RawMessage
	err := c.get(ctx, "config/calibration", &v)
	return v, err
}
func (c *Client) Distortion(ctx context.Context) (json.RawMessage, error) {
	var v json.RawMessage
	err := c.get(ctx, "config/distortion", &v)
	return v, err
}
func (c *Client) CalibrationEllipses(ctx context.Context) (json.RawMessage, error) {
	var v json.RawMessage
	err := c.get(ctx, "config/calibration/ellipses", &v)
	return v, err
}
func (c *Client) CameraResolution(ctx context.Context) (Resolution, error) {
	var v Resolution
	err := c.get(ctx, "config/cam/resolution", &v)
	return v, err
}

func (c *Client) Start(ctx context.Context) error { return c.action(ctx, http.MethodPut, "start", nil) }
func (c *Client) Stop(ctx context.Context) error  { return c.action(ctx, http.MethodPut, "stop", nil) }
func (c *Client) ResetVisit(ctx context.Context) error {
	return c.action(ctx, http.MethodPost, "reset", nil)
}
func (c *Client) Restart(ctx context.Context) error {
	return c.action(ctx, http.MethodPost, "restart", nil)
}
func (c *Client) StartStreaming(ctx context.Context) error {
	return c.action(ctx, http.MethodPut, "streams/start", nil)
}
func (c *Client) StopStreaming(ctx context.Context) error {
	return c.action(ctx, http.MethodPut, "streams/stop", nil)
}
func (c *Client) ConnectUpstream(ctx context.Context) error {
	return c.action(ctx, http.MethodPut, "upstream/connect", nil)
}
func (c *Client) DisconnectUpstream(ctx context.Context) error {
	return c.action(ctx, http.MethodPut, "upstream/disconnect", nil)
}
func (c *Client) AutoCalibrate(ctx context.Context) error {
	return c.action(ctx, http.MethodPost, "config/calibration/auto", nil)
}
func (c *Client) AutoCalibrateCamera(ctx context.Context, camera int) error {
	return c.action(ctx, http.MethodPost, fmt.Sprintf("config/calibration/auto/%d", camera), nil)
}
func (c *Client) PatchConfig(ctx context.Context, patch any) error {
	return c.action(ctx, http.MethodPatch, "config", patch)
}
func (c *Client) RevertConfig(ctx context.Context) error {
	return c.action(ctx, http.MethodPost, "config/revert", nil)
}
func (c *Client) ResetAuthentication(ctx context.Context) error {
	return c.action(ctx, http.MethodPost, "config/auth/reset", nil)
}
func (c *Client) SetCalibration(ctx context.Context, value any) error {
	return c.action(ctx, http.MethodPut, "config/calibration", value)
}
func (c *Client) SetDistortion(ctx context.Context, value any) error {
	return c.action(ctx, http.MethodPut, "config/distortion", value)
}
func (c *Client) SetDistortionEnabled(ctx context.Context, camera int, enabled bool) error {
	return c.action(ctx, http.MethodPut, fmt.Sprintf("config/distortion/%d/enabled", camera), enabled)
}
func (c *Client) SetDistortionAlpha(ctx context.Context, camera int, alpha float64) error {
	return c.action(ctx, http.MethodPut, fmt.Sprintf("config/distortion/%d/alpha", camera), alpha)
}
func (c *Client) RotateCalibrationLeft(ctx context.Context, camera int) error {
	return c.action(ctx, http.MethodPost, fmt.Sprintf("config/calibration/auto/%d/rotate-left", camera), nil)
}
func (c *Client) PatchCameraControls(ctx context.Context, camera int, patch any) error {
	return c.action(ctx, http.MethodPatch, fmt.Sprintf("cams/controls/%d", camera), patch)
}
func (c *Client) ResetCameraControls(ctx context.Context, camera int) error {
	return c.action(ctx, http.MethodPost, fmt.Sprintf("cams/controls/%d/reset", camera), nil)
}

func (c *Client) Events(ctx context.Context) (<-chan Event, <-chan error, error) {
	endpoint := *c.base
	if endpoint.Scheme == "https" {
		endpoint.Scheme = "wss"
	} else {
		endpoint.Scheme = "ws"
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api/events"
	conn, _, err := c.dialer.DialContext(ctx, endpoint.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	events, failures := make(chan Event, 32), make(chan error, 1)
	go func() {
		defer close(events)
		defer close(failures)
		defer conn.Close()
		go func() { <-ctx.Done(); conn.Close() }()
		for {
			var event Event
			if err := conn.ReadJSON(&event); err != nil {
				if ctx.Err() == nil {
					failures <- err
				}
				return
			}
			select {
			case events <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return events, failures, nil
}

func (c *Client) ApplyBoardEvent(event Event) (bool, error) {
	if event.Type != "state" {
		return false, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := json.Unmarshal(event.Data, &c.state.Board); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Client) ApplyPlayEvent(line []byte) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	changed, err := c.play.Apply(line)
	if err != nil || !changed {
		return changed, err
	}
	c.state.Variant, c.state.Remaining, c.state.Round = c.play.Variant, c.play.Remaining, c.play.Round
	c.state.MatchFinished, c.state.Winner = c.play.Finished, c.play.Winner
	c.state.CheckoutGuide = make([]Segment, len(c.play.CheckoutGuide))
	for i, s := range c.play.CheckoutGuide {
		c.state.CheckoutGuide[i] = Segment(s)
	}
	if c.play.LastThrow != nil {
		value := Segment(*c.play.LastThrow)
		c.state.LastThrow = &value
	}
	return true, nil
}

func (c *Client) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := c.state
	result.CheckoutGuide = append([]Segment(nil), result.CheckoutGuide...)
	return result
}
