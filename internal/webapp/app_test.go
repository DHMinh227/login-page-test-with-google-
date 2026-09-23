package webapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUnconfiguredAppStillServesSetupPage(t *testing.T) {
	app, err := New(context.Background(), Config{AppURL: "http://localhost:8080"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	app.Routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET / status = %d; want 200", recorder.Code)
	}
	if body := recorder.Body.String(); !contains(body, "One-time setup needed") {
		t.Fatalf("GET / did not contain setup guidance")
	}
}

func TestHealthReportsConfigurationState(t *testing.T) {
	app, err := New(context.Background(), Config{AppURL: "http://localhost:8080"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	app.Routes().ServeHTTP(recorder, request)

	var response struct {
		OK               bool `json:"ok"`
		GoogleConfigured bool `json:"google_configured"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.OK || response.GoogleConfigured {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func contains(text, part string) bool {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
