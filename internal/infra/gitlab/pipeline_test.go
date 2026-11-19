package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"forge-server/internal/domain"
	"forge-server/internal/service"
)

func newTestCIClient(t *testing.T, handler http.HandlerFunc) service.CIClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "glpat-token" {
			t.Errorf("expected private token header, got %q", r.Header.Get("PRIVATE-TOKEN"))
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	conn := testConnection()
	conn.APIBaseURL = server.URL
	client, err := NewProvider(false).CIClient(context.Background(), conn, domain.ModuleRepository{ExternalRepositoryID: "123"})
	if err != nil {
		t.Fatalf("create ci client: %v", err)
	}
	return client
}

func TestCIClientRejectsInvalidConnectionOrProject(t *testing.T) {
	provider := NewProvider(false)

	conn := testConnection()
	conn.Credential = " "
	if _, err := provider.CIClient(context.Background(), conn, domain.ModuleRepository{ExternalRepositoryID: "1"}); !errors.Is(err, domain.ErrCodeSourceCredentialInvalid) {
		t.Fatalf("expected credential error, got %v", err)
	}
	if _, err := provider.CIClient(context.Background(), testConnection(), domain.ModuleRepository{ExternalRepositoryID: "group/app"}); !errors.Is(err, domain.ErrInvalidCodeSourceInput) {
		t.Fatalf("expected invalid project id error, got %v", err)
	}
}

func TestCreatePipelineSendsRefAndVariables(t *testing.T) {
	client := newTestCIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v4/projects/123/pipeline" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Ref       string `json:"ref"`
			Variables []struct {
				Key          string `json:"key"`
				Value        string `json:"value"`
				VariableType string `json:"variable_type"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body.Ref != "main" || len(body.Variables) != 2 || body.Variables[0].Key != "FORGE_BUILD_ID" || body.Variables[0].VariableType != "env_var" || body.Variables[1].Value != "v2" {
			t.Errorf("unexpected body %#v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":42,"sha":"abc","status":"created","web_url":"https://gitlab.example.com/p/42"}`))
	})

	pipeline, err := client.CreatePipeline(context.Background(), "main", []domain.CIVariable{{Key: "FORGE_BUILD_ID", Value: "v1"}, {Key: "B", Value: "v2"}})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	if pipeline.ID != 42 || pipeline.SHA != "abc" || pipeline.Status != "created" || pipeline.URL != "https://gitlab.example.com/p/42" {
		t.Fatalf("unexpected pipeline %#v", pipeline)
	}
}

func TestCreatePipelineExplainsPermissionAndMissingProject(t *testing.T) {
	forbidden := newTestCIClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"403 Forbidden"}`))
	})
	_, err := forbidden.CreatePipeline(context.Background(), "main", nil)
	if err == nil || !strings.Contains(err.Error(), "api 权限") || !strings.Contains(err.Error(), "Developer") {
		t.Fatalf("expected permission hint, got %v", err)
	}

	missing := newTestCIClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	_, err = missing.CreatePipeline(context.Background(), "main", nil)
	if err == nil || errors.Is(err, domain.ErrCIPipelineNotFound) || !strings.Contains(err.Error(), "not found or the token has no access") {
		t.Fatalf("expected project access error rather than pipeline-not-found, got %v", err)
	}

	unauthorized := newTestCIClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	if _, err := unauthorized.CreatePipeline(context.Background(), "main", nil); !errors.Is(err, domain.ErrCodeSourceCredentialInvalid) {
		t.Fatalf("expected credential error, got %v", err)
	}
}

func TestGetPipelineMapsNotFound(t *testing.T) {
	client := newTestCIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/projects/123/pipelines/7" {
			_, _ = w.Write([]byte(`{"id":7,"sha":"def","status":"running","web_url":"u"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	pipeline, err := client.GetPipeline(context.Background(), 7)
	if err != nil || pipeline.Status != "running" || pipeline.SHA != "def" {
		t.Fatalf("unexpected pipeline %#v %v", pipeline, err)
	}
	if _, err := client.GetPipeline(context.Background(), 8); !errors.Is(err, domain.ErrCIPipelineNotFound) {
		t.Fatalf("expected ErrCIPipelineNotFound, got %v", err)
	}
}

func TestListPipelineJobsFollowsPagination(t *testing.T) {
	client := newTestCIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/123/pipelines/7/jobs" || r.URL.Query().Get("per_page") != "100" {
			t.Errorf("unexpected request %s", r.URL.String())
		}
		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("X-Next-Page", "2")
			_, _ = w.Write([]byte(`[{"id":1,"name":"lint","status":"success","web_url":"u1"}]`))
		case "2":
			_, _ = w.Write([]byte(`[{"id":2,"name":"forge_build","status":"running","web_url":"u2"}]`))
		default:
			t.Errorf("unexpected page %q", r.URL.Query().Get("page"))
		}
	})
	jobs, err := client.ListPipelineJobs(context.Background(), 7)
	if err != nil || len(jobs) != 2 || jobs[1].Name != "forge_build" || jobs[1].Status != "running" {
		t.Fatalf("unexpected jobs %#v %v", jobs, err)
	}
}

func TestJobTraceUsesRangeAndFallsBackToFullBody(t *testing.T) {
	const full = "0123456789abcdef"
	ranged := newTestCIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=10-" {
			t.Errorf("expected Range bytes=10-, got %q", got)
		}
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte(full[10:]))
	})
	data, err := ranged.JobTrace(context.Background(), 9, 10)
	if err != nil || string(data) != "abcdef" {
		t.Fatalf("unexpected ranged trace %q %v", data, err)
	}

	ignoresRange := newTestCIClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(full)) })
	data, err = ignoresRange.JobTrace(context.Background(), 9, 10)
	if err != nil || string(data) != "abcdef" {
		t.Fatalf("expected the first 10 bytes to be skipped, got %q %v", data, err)
	}
	data, err = ignoresRange.JobTrace(context.Background(), 9, 0)
	if err != nil || string(data) != full {
		t.Fatalf("expected the full trace at offset 0, got %q %v", data, err)
	}
	data, err = ignoresRange.JobTrace(context.Background(), 9, 99)
	if err != nil || len(data) != 0 {
		t.Fatalf("expected no data past the end, got %q %v", data, err)
	}
}

func TestJobTraceTreatsNoNewDataAsEmpty(t *testing.T) {
	for _, status := range []int{http.StatusRequestedRangeNotSatisfiable, http.StatusNotFound} {
		client := newTestCIClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		data, err := client.JobTrace(context.Background(), 9, 5)
		if err != nil || len(data) != 0 {
			t.Fatalf("status %d: expected empty trace, got %q %v", status, data, err)
		}
	}

	failing := newTestCIClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	if _, err := failing.JobTrace(context.Background(), 9, 0); err == nil {
		t.Fatal("expected server error to be reported")
	}
}

func TestDownloadJobArtifactsStreamsAndLimitsSize(t *testing.T) {
	client := newTestCIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/123/jobs/9/artifacts" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("zip-content"))
	})

	var buffer bytes.Buffer
	if err := client.DownloadJobArtifacts(context.Background(), 9, &buffer, 1024); err != nil || buffer.String() != "zip-content" {
		t.Fatalf("unexpected download %q %v", buffer.String(), err)
	}
	buffer.Reset()
	if err := client.DownloadJobArtifacts(context.Background(), 9, &buffer, 4); err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("expected size limit error, got %v", err)
	}

	missing := newTestCIClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	if err := missing.DownloadJobArtifacts(context.Background(), 9, &buffer, 1024); !errors.Is(err, domain.ErrCIArtifactsNotFound) {
		t.Fatalf("expected ErrCIArtifactsNotFound, got %v", err)
	}
}

func TestCancelPipelineIgnoresMissingPipeline(t *testing.T) {
	var called bool
	client := newTestCIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v4/projects/123/pipelines/7/cancel" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		called = true
		_, _ = w.Write([]byte(`{"id":7}`))
	})
	if err := client.CancelPipeline(context.Background(), 7); err != nil || !called {
		t.Fatalf("expected cancel to be sent, called=%v err=%v", called, err)
	}

	gone := newTestCIClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	if err := gone.CancelPipeline(context.Background(), 7); err != nil {
		t.Fatalf("expected a missing pipeline to be ignored, got %v", err)
	}
}
