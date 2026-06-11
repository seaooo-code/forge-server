package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"forge-server/internal/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	logglobal "go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTelemetryLogsHTTPPayloads(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	traceProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	previousTraceProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(traceProvider)

	logExporter := &memoryLogExporter{}
	logProvider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(logExporter)))
	previousLogProvider := logglobal.GetLoggerProvider()
	logglobal.SetLoggerProvider(logProvider)

	t.Cleanup(func() {
		_ = logProvider.Shutdown(context.Background())
		logglobal.SetLoggerProvider(previousLogProvider)
		_ = traceProvider.Shutdown(context.Background())
		otel.SetTracerProvider(previousTraceProvider)
	})

	e := New(config.Config{
		Telemetry: config.TelemetryConfig{
			Enabled:     true,
			ServiceName: "forge-server",
		},
	}, Dependencies{})

	requestBody := `{"password":"plain"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout?token=query-token", strings.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer raw-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d: %s", http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	}

	assertSpanDoesNotContainHTTPPayloads(t, spanRecorder.Ended())

	logRecords := logExporter.Records()
	if len(logRecords) != 1 {
		t.Fatalf("expected 1 log record, got %d", len(logRecords))
	}

	record := logRecords[0]
	if record.EventName() != "http.exchange" {
		t.Fatalf("expected http.exchange event, got %q", record.EventName())
	}
	if !record.TraceID().IsValid() {
		t.Fatal("expected log record to include trace id")
	}
	if !record.SpanID().IsValid() {
		t.Fatal("expected log record to include span id")
	}

	body := valueMap(t, record.Body())
	request := mapValue(t, body, "request")
	response := mapValue(t, body, "response")

	requestPayload := mapValue(t, request, "body")
	if got := mapLookup(t, requestPayload, "password").AsString(); got != "plain" {
		t.Fatalf("expected structured request body password, got %q", got)
	}

	requestHeaders := mapValue(t, request, "headers")
	authValues := mapLookup(t, requestHeaders, "Authorization").AsSlice()
	if len(authValues) != 1 || authValues[0].AsString() != "Bearer raw-token" {
		t.Fatalf("expected raw authorization header, got %#v", authValues)
	}

	query := mapValue(t, request, "query")
	queryParams := mapValue(t, query, "params")
	tokenValues := mapLookup(t, queryParams, "token").AsSlice()
	if len(tokenValues) != 1 || tokenValues[0].AsString() != "query-token" {
		t.Fatalf("expected raw query token, got %#v", tokenValues)
	}

	responsePayload := mapValue(t, response, "body")
	if got := mapLookup(t, responsePayload, "error").AsString(); got != "feishu auth unavailable" {
		t.Fatalf("expected structured response error, got %q", got)
	}
	if got := mapLookup(t, response, "statusCode").AsInt64(); got != int64(http.StatusServiceUnavailable) {
		t.Fatalf("expected response status %d, got %d", http.StatusServiceUnavailable, got)
	}
}

type memoryLogExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (e *memoryLogExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i := range records {
		e.records = append(e.records, records[i].Clone())
	}
	return nil
}

func (e *memoryLogExporter) Shutdown(context.Context) error {
	return nil
}

func (e *memoryLogExporter) ForceFlush(context.Context) error {
	return nil
}

func (e *memoryLogExporter) Records() []sdklog.Record {
	e.mu.Lock()
	defer e.mu.Unlock()

	records := make([]sdklog.Record, len(e.records))
	copy(records, e.records)
	return records
}

func assertSpanDoesNotContainHTTPPayloads(t *testing.T, spans []sdktrace.ReadOnlySpan) {
	t.Helper()

	for _, span := range spans {
		attributes := make(map[attribute.Key]attribute.Value, len(span.Attributes()))
		for _, attr := range span.Attributes() {
			attributes[attr.Key] = attr.Value
		}
		for _, key := range []attribute.Key{
			"http.request.headers",
			"http.request.body",
			"http.response.headers",
			"http.response.body",
		} {
			if _, ok := attributes[key]; ok {
				t.Fatalf("span %q should not contain %s", span.Name(), key)
			}
		}
	}
}

func mapValue(t *testing.T, items []otellog.KeyValue, key string) []otellog.KeyValue {
	t.Helper()

	return valueMap(t, mapLookup(t, items, key))
}

func valueMap(t *testing.T, value otellog.Value) []otellog.KeyValue {
	t.Helper()

	items := value.AsMap()
	if items == nil {
		t.Fatalf("expected map value, got %s", value.Kind())
	}
	return items
}

func mapLookup(t *testing.T, items []otellog.KeyValue, key string) otellog.Value {
	t.Helper()

	for _, item := range items {
		if item.Key == key {
			return item.Value
		}
	}
	t.Fatalf("expected map key %q in %#v", key, items)
	return otellog.Value{}
}
