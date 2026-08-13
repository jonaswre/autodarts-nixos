package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

var uuidPattern = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

type service struct {
	bind                                     string
	port                                     int
	boardURL, stateDir, assetDir, advertised string
	client                                   *http.Client
	mu                                       sync.Mutex
	calibration, lastError                   string
	playCapture                              bool
	playEvents, playMessages                 int
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func main() {
	port, err := strconv.Atoi(env("AUTODARTS_ONBOARDING_PORT", "3182"))
	if err != nil {
		log.Fatal(err)
	}
	s := &service{bind: env("AUTODARTS_ONBOARDING_BIND", "0.0.0.0"), port: port, boardURL: strings.TrimRight(env("AUTODARTS_BOARD_MANAGER_URL", "http://127.0.0.1:3180/api"), "/"), stateDir: env("AUTODARTS_ONBOARDING_STATE", "/var/lib/autodarts-onboarding"), assetDir: env("AUTODARTS_ONBOARDING_ASSETS", "."), advertised: os.Getenv("AUTODARTS_ONBOARDING_ADVERTISED_HOST"), client: &http.Client{Timeout: 20 * time.Second}, calibration: "idle"}
	server := &http.Server{Addr: net.JoinHostPort(s.bind, strconv.Itoa(s.port)), Handler: s, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}

func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'")
	if r.Method == http.MethodGet {
		s.get(w, r)
		return
	}
	if r.Method == http.MethodPost {
		s.post(w, r)
		return
	}
	s.reply(w, 405, map[string]any{"error": "Method not allowed."})
}
func (s *service) local(w http.ResponseWriter, r *http.Request) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host != "127.0.0.1" && host != "::1" {
		s.reply(w, 403, map[string]any{"error": "Pairing details are only shown on the appliance display."})
		return false
	}
	return true
}
func (s *service) reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if value != nil {
		json.NewEncoder(w).Encode(value)
	}
}
func (s *service) asset(w http.ResponseWriter, name string) {
	data, err := os.ReadFile(filepath.Join(s.assetDir, name))
	if err != nil {
		s.reply(w, 500, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}
func (s *service) get(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/", "/device":
		if s.local(w, r) {
			s.asset(w, "device.html")
		}
	case "/setup":
		s.asset(w, "setup.html")
	case "/qr.svg", "/remote-control-qr.svg":
		if !s.local(w, r) {
			return
		}
		target := s.setupURL()
		if r.URL.Path == "/remote-control-qr.svg" {
			target = s.remoteURL()
		}
		png, err := qrcode.Encode(target, qrcode.Medium, 256)
		if err != nil {
			s.reply(w, 500, map[string]any{"error": err.Error()})
			return
		}
		svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 256 256"><image width="256" height="256" href="data:image/png;base64,%s"/></svg>`, base64.StdEncoding.EncodeToString(png))
		w.Header().Set("Content-Type", "image/svg+xml")
		io.WriteString(w, svg)
	case "/api/pairing":
		if !s.local(w, r) {
			return
		}
		configured, _ := s.configured()
		s.reply(w, 200, map[string]any{"configured": configured, "url": s.setupURL()})
	case "/api/status":
		configured, ready := s.configured()
		s.mu.Lock()
		value := map[string]any{"ready": ready, "configured": configured, "calibration": s.calibration, "error": s.lastError}
		s.mu.Unlock()
		s.reply(w, 200, value)
	case "/api/play-state":
		if !s.local(w, r) {
			return
		}
		data, err := os.ReadFile(s.file("play-state.json"))
		if errors.Is(err, os.ErrNotExist) {
			s.reply(w, 404, map[string]any{"error": "No Play WebSocket state received yet."})
			return
		}
		if err != nil {
			s.reply(w, 500, map[string]any{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	case "/api/play-capture/status":
		if !s.local(w, r) {
			return
		}
		s.mu.Lock()
		value := map[string]any{"connected": s.playMessages > 0, "messages": s.playMessages, "capture": s.playCapture, "events": s.playEvents}
		s.mu.Unlock()
		s.reply(w, 200, value)
	case "/api/configured", "/api/setup-required":
		configured, ready := s.configured()
		status := 409
		if !ready {
			status = 503
		} else if (r.URL.Path == "/api/configured" && configured) || (r.URL.Path == "/api/setup-required" && !configured) {
			status = 204
		}
		w.WriteHeader(status)
	default:
		s.reply(w, 404, map[string]any{"error": "Not found."})
	}
}
func (s *service) post(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/play-capture/start", "/api/play-capture/stop", "/api/play-event":
		if !s.local(w, r) {
			return
		}
		s.playPost(w, r)
	case "/api/setup":
		s.setup(w, r)
	default:
		s.reply(w, 404, map[string]any{"error": "Not found."})
	}
}
func (s *service) board(method, path string, body, target any, timeout time.Duration) error {
	var input io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, s.boardURL+path, input)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := *s.client
	client.Timeout = timeout
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("Board Manager: %s", res.Status)
	}
	if target != nil {
		return json.NewDecoder(res.Body).Decode(target)
	}
	io.Copy(io.Discard, res.Body)
	return nil
}
func (s *service) configured() (bool, bool) {
	var config struct {
		Auth struct {
			BoardID string `json:"board_id"`
		} `json:"auth"`
	}
	if s.board("GET", "/config", nil, &config, 3*time.Second) != nil {
		return false, false
	}
	return config.Auth.BoardID != "", true
}

func (s *service) waitForBoardID(boardID string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		var config struct {
			Auth struct {
				BoardID string `json:"board_id"`
			} `json:"auth"`
		}
		if s.board("GET", "/config", nil, &config, 3*time.Second) == nil && config.Auth.BoardID == boardID {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(500 * time.Millisecond)
	}
}
func (s *service) file(name string) string { return filepath.Join(s.stateDir, name) }
func (s *service) token() (string, error) {
	if data, err := os.ReadFile(s.file("pairing-token")); err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(data)
	return token, s.write("pairing-token", []byte(token))
}
func (s *service) address() string {
	if s.advertised != "" {
		return s.advertised
	}
	conn, err := net.Dial("udp", "1.1.1.1:80")
	if err != nil {
		return "autodarts"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
func (s *service) setupURL() string {
	token, _ := s.token()
	return fmt.Sprintf("http://%s:%d/setup?token=%s", s.address(), s.port, token)
}
func (s *service) remoteURL() string {
	return fmt.Sprintf("https://%s:6080/vnc.html?autoconnect=true&show_dot=true&resize=scale", s.address())
}
func (s *service) write(name string, data []byte) error {
	if err := os.MkdirAll(s.stateDir, 0700); err != nil {
		return err
	}
	path := s.file(name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

type device struct {
	Bus     string `json:"bus"`
	Formats []struct {
		Path string `json:"path"`
	} `json:"formats"`
}

func (s *service) cameras() ([]string, error) {
	var devices []device
	if err := s.board("GET", "/devices", nil, &devices, 20*time.Second); err != nil {
		return nil, err
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Bus < devices[j].Bus })
	var result []string
	for _, d := range devices {
		if len(d.Formats) > 0 && d.Formats[0].Path != "" {
			result = append(result, d.Formats[0].Path)
		}
	}
	if len(result) < 3 {
		return nil, fmt.Errorf("Expected three cameras, found %d.", len(result))
	}
	return result[:3], nil
}

type setupRequest struct {
	Token   string `json:"token"`
	BoardID string `json:"board_id"`
	APIKey  string `json:"api_key"`
}

func (s *service) setup(w http.ResponseWriter, r *http.Request) {
	var data setupRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&data); err != nil {
		s.reply(w, 400, map[string]any{"error": "Invalid request size."})
		return
	}
	token, err := s.token()
	if err != nil {
		s.reply(w, 400, map[string]any{"error": err.Error()})
		return
	}
	if _, err := os.Stat(s.file("pairing-token")); err != nil || len(token) != len(data.Token) || subtle.ConstantTimeCompare([]byte(token), []byte(data.Token)) != 1 {
		s.reply(w, 403, map[string]any{"error": "Pairing code is invalid or has expired."})
		return
	}
	if !uuidPattern.MatchString(data.BoardID) || uuidPattern.FindString(data.BoardID) != data.BoardID {
		s.reply(w, 400, map[string]any{"error": "Board ID format is invalid."})
		return
	}
	if len(data.APIKey) < 16 || len(data.APIKey) > 2048 || strings.IndexFunc(data.APIKey, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' }) >= 0 {
		s.reply(w, 400, map[string]any{"error": "API key format is invalid."})
		return
	}
	cams, err := s.cameras()
	if err != nil {
		s.reply(w, 400, map[string]any{"error": err.Error()})
		return
	}
	// Detection 1.0.7 panics if automatic calibration is enabled while its
	// persisted calibration map is still nil. Its calibration GET returns a
	// valid three-camera baseline, so persist that before invoking auto-calibration.
	var baseline json.RawMessage
	if err := s.board("GET", "/config/calibration", nil, &baseline, 20*time.Second); err != nil {
		s.reply(w, 400, map[string]any{"error": err.Error()})
		return
	}
	if err := s.board("PUT", "/config/calibration", baseline, nil, 20*time.Second); err != nil {
		s.reply(w, 400, map[string]any{"error": err.Error()})
		return
	}
	patch := map[string]any{"auth": map[string]any{"board_id": data.BoardID, "api_key": data.APIKey}, "cam": map[string]any{"cams": cams, "width": 1280, "height": 720, "fps": 30, "rotate_180": []bool{false, false, false}, "auto_calibrate": false, "auto_calibrate_on_start": false, "auto_distortion": true}}
	var config struct {
		Auth struct {
			BoardID string `json:"board_id"`
		} `json:"auth"`
	}
	if err := s.board("PATCH", "/config", patch, &config, 20*time.Second); err != nil {
		// Board Manager may persist the configuration and restart before it can
		// finish the PATCH response. Reconcile the observable state before
		// reporting an error or leaving the pairing token active.
		if !s.waitForBoardID(data.BoardID, 20*time.Second) {
			s.reply(w, 400, map[string]any{"error": err.Error()})
			return
		}
		config.Auth.BoardID = data.BoardID
	}
	if config.Auth.BoardID != data.BoardID {
		s.reply(w, 400, map[string]any{"error": "Board Manager did not retain the Board ID."})
		return
	}
	os.Remove(s.file("pairing-token"))
	s.mu.Lock()
	s.calibration = "running"
	s.lastError = ""
	s.mu.Unlock()
	go func() {
		err := s.board("POST", "/config/calibration/auto?distortion=true", nil, nil, 180*time.Second)
		s.mu.Lock()
		defer s.mu.Unlock()
		if err != nil {
			s.calibration = "failed"
			s.lastError = err.Error()
		} else {
			s.calibration = "complete"
		}
	}()
	s.reply(w, 200, map[string]any{"ok": true, "cameras": len(cams), "calibration": "running"})
}

func (s *service) playPost(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.URL.Path == "/api/play-capture/start" {
		os.Remove(s.file("play-events.jsonl"))
		s.playCapture = true
		s.playEvents = 0
		s.reply(w, 200, map[string]any{"capture": true, "events": 0})
		return
	}
	if r.URL.Path == "/api/play-capture/stop" {
		s.playCapture = false
		s.reply(w, 200, map[string]any{"capture": false, "events": s.playEvents})
		return
	}
	var payload struct {
		URL       string `json:"url"`
		Data      string `json:"data"`
		Transport string `json:"transport"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4100000)
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.reply(w, 400, map[string]any{"error": "Invalid Play WebSocket event."})
		return
	}
	if err := s.ingest(payload.URL, payload.Data, payload.Transport); err != nil {
		s.reply(w, 400, map[string]any{"error": err.Error()})
		return
	}
	w.WriteHeader(204)
}
func (s *service) alias(kind string, value any) string {
	salt, err := os.ReadFile(s.file("play-salt"))
	if err != nil {
		salt = make([]byte, 32)
		rand.Read(salt)
		s.write("play-salt", salt)
	}
	sum := sha256.Sum256(append(append(salt, []byte(kind)...), []byte(fmt.Sprint(value))...))
	return fmt.Sprintf("[%s-%s]", kind, hex.EncodeToString(sum[:6]))
}
func (s *service) sanitize(value any) any {
	switch v := value.(type) {
	case []any:
		for i := range v {
			v[i] = s.sanitize(v[i])
		}
		return v
	case string:
		return uuidPattern.ReplaceAllStringFunc(v, func(x string) string { return s.alias("id", x) })
	case map[string]any:
		clean := map[string]any{}
		_, bed := v["bed"]
		_, mul := v["multiplier"]
		_, num := v["number"]
		segment := bed && mul && num
		for key, child := range v {
			safe := uuidPattern.ReplaceAllStringFunc(key, func(x string) string { return s.alias("id", x) })
			lower := strings.ToLower(key)
			switch {
			case strings.Contains(lower, "token") || strings.Contains(lower, "authorization") || strings.Contains(lower, "cookie") || strings.Contains(lower, "password") || strings.Contains(lower, "api_key"):
				clean[safe] = "[redacted]"
			case lower == "email":
				clean[safe] = s.alias("email", child)
			case (lower == "player" || lower == "user" || lower == "host" || lower == "board") && reflectString(child):
				clean[safe] = s.alias("name", child)
			case strings.HasSuffix(lower, "name") && !segment:
				clean[safe] = s.alias("name", child)
			case strings.HasSuffix(lower, "url"):
				clean[safe] = "[redacted-url]"
			case lower == "id" || strings.HasSuffix(lower, "_id") || strings.HasSuffix(lower, "id"):
				clean[safe] = s.alias("id", child)
			default:
				clean[safe] = s.sanitize(child)
			}
		}
		return clean
	default:
		return value
	}
}
func reflectString(value any) bool { _, ok := value.(string); return ok }
func (s *service) ingest(rawURL, data, transport string) error {
	if len(data) > 4000000 {
		return errors.New("Play WebSocket event is too large.")
	}
	source, err := url.Parse(rawURL)
	if err != nil || (source.Scheme != "http" && source.Scheme != "https" && source.Scheme != "ws" && source.Scheme != "wss") {
		return errors.New("Invalid Play event URL.")
	}
	var message any
	if json.Unmarshal([]byte(data), &message) != nil {
		message = map[string]any{"text": data}
	}
	sourceText := source.Scheme + "://" + source.Hostname() + source.Path
	sourceText = uuidPattern.ReplaceAllStringFunc(sourceText, func(x string) string { return s.alias("id", x) })
	kind := "websocket"
	if transport == "http-response" {
		kind = transport
	}
	event := map[string]any{"captured_at": time.Now().UTC().Format(time.RFC3339Nano), "source": sourceText, "transport": kind, "message": s.sanitize(message)}
	encoded, _ := json.Marshal(event)
	encoded = append(encoded, '\n')
	if err := s.write("play-state.json", encoded); err != nil {
		return err
	}
	s.playMessages++
	if s.playCapture {
		file, err := os.OpenFile(s.file("play-events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = file.Write(encoded)
		file.Close()
		if err != nil {
			return err
		}
		s.playEvents++
	}
	return nil
}
