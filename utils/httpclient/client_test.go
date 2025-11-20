package httpclient

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", config.Timeout)
	}

	if config.RetryCount != 3 {
		t.Errorf("Expected retry count 3, got %d", config.RetryCount)
	}

	if config.Headers == nil {
		t.Error("Expected headers map to be initialized")
	}
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
	}{
		{
			name:   "with nil config",
			config: nil,
		},
		{
			name: "with custom config",
			config: &Config{
				Timeout:    10 * time.Second,
				RetryCount: 5,
				BaseURL:    "https://api.example.com",
				Headers: map[string]string{
					"User-Agent": "test-agent",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.config)

			if client == nil {
				t.Fatal("Expected non-nil client")
			}

			if client.resty == nil {
				t.Error("Expected resty client to be initialized")
			}

			if client.config == nil {
				t.Error("Expected config to be initialized")
			}
		})
	}
}

func TestClient_R(t *testing.T) {
	client := NewClient(nil)
	req := client.R()

	if req == nil {
		t.Fatal("Expected non-nil request")
	}
}

func TestClient_Resty(t *testing.T) {
	client := NewClient(nil)
	restyClient := client.Resty()

	if restyClient == nil {
		t.Fatal("Expected non-nil resty client")
	}
}

func TestClient_SetTimeout(t *testing.T) {
	client := NewClient(nil)
	newTimeout := 60 * time.Second

	result := client.SetTimeout(newTimeout)

	if result != client {
		t.Error("Expected method chaining to return client")
	}

	if client.config.Timeout != newTimeout {
		t.Errorf("Expected timeout %v, got %v", newTimeout, client.config.Timeout)
	}
}

func TestClient_SetRetryCount(t *testing.T) {
	client := NewClient(nil)
	newRetryCount := 5

	result := client.SetRetryCount(newRetryCount)

	if result != client {
		t.Error("Expected method chaining to return client")
	}

	if client.config.RetryCount != newRetryCount {
		t.Errorf("Expected retry count %d, got %d", newRetryCount, client.config.RetryCount)
	}
}

func TestClient_SetHeader(t *testing.T) {
	client := NewClient(nil)

	result := client.SetHeader("Authorization", "Bearer token")

	if result != client {
		t.Error("Expected method chaining to return client")
	}

	if client.config.Headers["Authorization"] != "Bearer token" {
		t.Error("Expected header to be set")
	}
}

func TestClient_SetHeaders(t *testing.T) {
	client := NewClient(nil)
	headers := map[string]string{
		"X-Custom-Header": "value1",
		"X-Another-Header": "value2",
	}

	result := client.SetHeaders(headers)

	if result != client {
		t.Error("Expected method chaining to return client")
	}

	for k, v := range headers {
		if client.config.Headers[k] != v {
			t.Errorf("Expected header %s=%s, got %s", k, v, client.config.Headers[k])
		}
	}
}

func TestClient_SetBaseURL(t *testing.T) {
	client := NewClient(nil)
	baseURL := "https://api.example.com"

	result := client.SetBaseURL(baseURL)

	if result != client {
		t.Error("Expected method chaining to return client")
	}

	if client.config.BaseURL != baseURL {
		t.Errorf("Expected base URL %s, got %s", baseURL, client.config.BaseURL)
	}
}

func TestClient_SetDebug(t *testing.T) {
	client := NewClient(nil)

	result := client.SetDebug(true)

	if result != client {
		t.Error("Expected method chaining to return client")
	}

	if !client.config.Debug {
		t.Error("Expected debug to be true")
	}
}

func TestClient_Config(t *testing.T) {
	originalConfig := &Config{
		Timeout:    10 * time.Second,
		RetryCount: 5,
		Headers: map[string]string{
			"X-Test": "value",
		},
	}

	client := NewClient(originalConfig)
	configCopy := client.Config()

	// Verify it's a copy
	if configCopy == client.config {
		t.Error("Expected Config() to return a copy, not the original")
	}

	// Verify values match
	if configCopy.Timeout != originalConfig.Timeout {
		t.Error("Expected timeout to match")
	}

	if configCopy.RetryCount != originalConfig.RetryCount {
		t.Error("Expected retry count to match")
	}

	// Modify the copy - should not affect original
	configCopy.Headers["X-Test"] = "modified"
	if client.config.Headers["X-Test"] == "modified" {
		t.Error("Modifying config copy should not affect original")
	}
}

func TestDefault(t *testing.T) {
	// Reset to ensure clean state
	ResetDefault()

	client1 := Default()
	client2 := Default()

	if client1 == nil {
		t.Fatal("Expected non-nil default client")
	}

	if client1 != client2 {
		t.Error("Expected Default() to return the same instance")
	}
}

func TestSetDefault(t *testing.T) {
	customClient := NewClient(&Config{
		Timeout: 5 * time.Second,
	})

	SetDefault(customClient)
	defaultClient := Default()

	if defaultClient != customClient {
		t.Error("Expected default client to be the custom client")
	}

	// Clean up
	ResetDefault()
}

func TestResetDefault(t *testing.T) {
	client1 := Default()
	ResetDefault()
	client2 := Default()

	if client1 == client2 {
		t.Error("Expected ResetDefault() to create a new instance")
	}
}

func TestClient_HTTPRequest(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "success"}`))
	}))
	defer server.Close()

	client := NewClient(nil)
	resp, err := client.R().Get(server.URL)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if resp.StatusCode() != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode())
	}

	if string(resp.Body()) != `{"message": "success"}` {
		t.Errorf("Unexpected response body: %s", string(resp.Body()))
	}
}

func TestClient_MethodChaining(t *testing.T) {
	client := NewClient(nil).
		SetTimeout(20 * time.Second).
		SetRetryCount(2).
		SetHeader("User-Agent", "test").
		SetBaseURL("https://example.com").
		SetDebug(false)

	if client.config.Timeout != 20*time.Second {
		t.Error("Method chaining failed for SetTimeout")
	}

	if client.config.RetryCount != 2 {
		t.Error("Method chaining failed for SetRetryCount")
	}

	if client.config.Headers["User-Agent"] != "test" {
		t.Error("Method chaining failed for SetHeader")
	}

	if client.config.BaseURL != "https://example.com" {
		t.Error("Method chaining failed for SetBaseURL")
	}
}
