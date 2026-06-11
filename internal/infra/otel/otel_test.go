package otel

import "testing"

func TestNormalizeOTLPEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
	}{
		{
			name:     "empty endpoint",
			endpoint: "",
			want:     "http://127.0.0.1:4318/v1/traces",
		},
		{
			name:     "host port endpoint",
			endpoint: "127.0.0.1:4318",
			want:     "http://127.0.0.1:4318/v1/traces",
		},
		{
			name:     "base url endpoint",
			endpoint: "http://127.0.0.1:4318",
			want:     "http://127.0.0.1:4318/v1/traces",
		},
		{
			name:     "trace url endpoint",
			endpoint: "http://127.0.0.1:4318/v1/traces",
			want:     "http://127.0.0.1:4318/v1/traces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeOTLPEndpoint(tt.endpoint, "/v1/traces")
			if got != tt.want {
				t.Fatalf("expected endpoint %q, got %q", tt.want, got)
			}
		})
	}
}

func TestNormalizeOTLPLogEndpoint(t *testing.T) {
	got := normalizeOTLPEndpoint("http://127.0.0.1:4318", "/v1/logs")
	if got != "http://127.0.0.1:4318/v1/logs" {
		t.Fatalf("expected log endpoint, got %q", got)
	}
}
