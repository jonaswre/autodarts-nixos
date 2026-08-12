package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeJSONPreservesShapeAndRemovesSecrets(t *testing.T) {
	input := []byte(`{"auth":{"board_id":"uuid","api_key":"secret"},"host":{"ip":"10.0.0.2","hostname":"dart"},"cam":{"cams":["/dev/video0"]},"device":{"bus":"usb-1","path":"/dev/video2"},"value":42}`)
	result, err := sanitizeJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	text := string(result)
	for _, secret := range []string{"uuid", "secret", "10.0.0.2", "/dev/video0", "/dev/video2", "usb-1"} {
		if strings.Contains(text, secret) {
			t.Fatalf("secret %q remains in %s", secret, text)
		}
	}
	var decoded map[string]any
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["value"] != float64(42) {
		t.Fatalf("non-secret value changed: %#v", decoded)
	}
}

func TestPlainTextBecomesJSONString(t *testing.T) {
	result, err := sanitizeJSON([]byte("1.0.7"))
	if err != nil || string(result) != "\"1.0.7\"\n" {
		t.Fatalf("got %q, %v", result, err)
	}
}
