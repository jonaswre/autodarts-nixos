package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var endpoints = []string{
	"host", "version", "state", "state/motion", "state/stats", "cams/state",
	"cams/stats", "config", "config/cam/resolution", "config/calibration",
	"config/distortion", "config/calibration/ellipses", "devices",
}

type manifest struct {
	CapturedAt   time.Time `json:"captured_at"`
	BaseURL      string    `json:"base_url"`
	Duration     string    `json:"event_duration"`
	HTTPFixtures int       `json:"http_fixtures"`
	EventCount   int       `json:"event_count"`
	Redacted     bool      `json:"redacted"`
}

func main() {
	base := flag.String("url", "http://autodarts:3180", "Board Manager base URL")
	out := flag.String("out", "protocol/testdata/captures/latest", "output directory")
	duration := flag.Duration("duration", 30*time.Second, "WebSocket capture duration")
	flag.Parse()

	if err := capture(context.Background(), *base, *out, *duration); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func capture(ctx context.Context, base, out string, duration time.Duration) error {
	parsed, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("invalid board URL %q", base)
	}
	if err := os.MkdirAll(filepath.Join(out, "http"), 0o755); err != nil {
		return err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	count := 0
	for _, endpoint := range endpoints {
		requestURL := strings.TrimRight(base, "/") + "/api/" + endpoint
		response, err := client.Get(requestURL)
		if err != nil {
			return fmt.Errorf("GET %s: %w", endpoint, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 32<<20))
		response.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read %s: %w", endpoint, readErr)
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("GET %s: %s", endpoint, response.Status)
		}
		fixture, err := sanitizeJSON(body)
		if err != nil {
			return fmt.Errorf("sanitize %s: %w", endpoint, err)
		}
		name := strings.ReplaceAll(endpoint, "/", "-") + ".json"
		if err := os.WriteFile(filepath.Join(out, "http", name), fixture, 0o644); err != nil {
			return err
		}
		count++
	}

	events, err := captureEvents(ctx, parsed, filepath.Join(out, "events.jsonl"), duration)
	if err != nil {
		return err
	}
	metadata, _ := json.MarshalIndent(manifest{
		CapturedAt: time.Now().UTC(), BaseURL: parsed.Scheme + "://[board]", Duration: duration.String(),
		HTTPFixtures: count, EventCount: events, Redacted: true,
	}, "", "  ")
	metadata = append(metadata, '\n')
	return os.WriteFile(filepath.Join(out, "manifest.json"), metadata, 0o644)
}

func captureEvents(ctx context.Context, base *url.URL, path string, duration time.Duration) (int, error) {
	wsURL := *base
	if wsURL.Scheme == "https" {
		wsURL.Scheme = "wss"
	} else {
		wsURL.Scheme = "ws"
	}
	wsURL.Path = strings.TrimRight(wsURL.Path, "/") + "/api/events"

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("connect events: %w", err)
	}
	defer conn.Close()
	file, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	deadline := time.Now().Add(duration)
	if err := conn.SetReadDeadline(deadline); err != nil {
		return 0, err
	}
	count := 0
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			var netErr interface{ Timeout() bool }
			if errors.As(err, &netErr) && netErr.Timeout() {
				return count, nil
			}
			return count, fmt.Errorf("read events: %w", err)
		}
		clean, err := sanitizeJSON(payload)
		if err != nil {
			return count, fmt.Errorf("sanitize event: %w", err)
		}
		if _, err := file.Write(clean); err != nil {
			return count, err
		}
		count++
	}
}

func sanitizeJSON(input []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(input, &value); err != nil {
		// Plain-text endpoints such as /version are represented as JSON strings.
		value = strings.TrimSpace(string(input))
	}
	sanitize(value)
	out, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func sanitize(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch strings.ToLower(key) {
			case "api_key", "api-key", "token", "password", "tls_key", "tls_cert", "board_id", "boardid":
				typed[key] = "[redacted]"
			case "ip", "hostname":
				typed[key] = "[board]"
			case "bus":
				typed[key] = "[usb-bus]"
			case "path":
				if path, ok := child.(string); ok && strings.HasPrefix(path, "/dev/") {
					typed[key] = "[device-path]"
				} else {
					sanitize(child)
				}
			case "cams":
				if list, ok := child.([]any); ok {
					for index := range list {
						list[index] = fmt.Sprintf("[camera-%d]", index)
					}
				}
			default:
				sanitize(child)
			}
		}
	case []any:
		for _, child := range typed {
			sanitize(child)
		}
	}
}
