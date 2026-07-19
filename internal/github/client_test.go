package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLatestPublishedReleaseSkipsDraft(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/widget/releases" || r.URL.Query().Get("per_page") == "" {
			t.Fatalf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[
			{"id":43,"tag_name":"v2-draft","name":"draft","draft":true,"body":"hidden"},
			{"id":42,"tag_name":"v1.4","name":"Widget v1.4","draft":false,"prerelease":false,"body":"CSV export","html_url":"https://github.test/r/42","published_at":"2026-07-18T10:00:00Z"}
		]`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	release, err := client.LatestPublishedRelease(context.Background(), "acme/widget")
	if err != nil {
		t.Fatal(err)
	}
	if release.ID != 42 || release.Tag != "v1.4" || release.Body != "CSV export" {
		t.Fatalf("release = %+v", release)
	}
}

func TestRepositoryFileAndIssueCreation(t *testing.T) {
	t.Parallel()
	var issueRequest CreateIssueRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widget/contents/README.md":
			_, _ = w.Write([]byte(`{"name":"README.md","path":"README.md","encoding":"base64","content":"IyBXaWRnZXQ="}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/approvals/issues":
			if r.Header.Get("Authorization") != "Bearer secret" {
				t.Fatalf("auth = %q", r.Header.Get("Authorization"))
			}
			if err := json.NewDecoder(r.Body).Decode(&issueRequest); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":9001,"number":7,"html_url":"https://github.test/acme/approvals/issues/7","title":"approval","body":"marker","state":"open"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	content, err := client.ReadFile(context.Background(), "acme/widget", "README.md", "")
	if err != nil || string(content) != "# Widget" {
		t.Fatalf("ReadFile = %q, %v", content, err)
	}
	issue, err := client.CreateIssue(context.Background(), "acme/approvals", CreateIssueRequest{Title: "Approval", Body: "<!-- marker -->", Labels: []string{"marketing"}})
	if err != nil {
		t.Fatal(err)
	}
	if issue.ID != 9001 || issue.Number != 7 || issueRequest.Labels[0] != "marketing" {
		t.Fatalf("issue=%+v request=%+v", issue, issueRequest)
	}
}

func TestFindIssueByMarkerExcludesPullRequestsAndSearchesAllStates(t *testing.T) {
	t.Parallel()
	const marker = "<!-- marketing-os-approval:abc -->"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/approvals/issues" || r.URL.Query().Get("state") != "all" {
			t.Fatalf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[
			{"id":1,"number":1,"body":"` + marker + `","pull_request":{"url":"x"}},
			{"id":2,"number":2,"html_url":"https://github.test/i/2","title":"found","body":"before ` + marker + ` after","state":"closed"}
		]`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	issue, found, err := client.FindIssueByMarker(context.Background(), "acme/approvals", marker)
	if err != nil {
		t.Fatal(err)
	}
	if !found || issue.ID != 2 || issue.State != "closed" {
		t.Fatalf("found=%t issue=%+v", found, issue)
	}
}

func TestGitHubRetriesRateLimitWithoutLeakingToken(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "do not echo secret", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"id":10,"full_name":"acme/widget","default_branch":"main"}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	_, err := client.Repository(context.Background(), "acme/widget")
	if err != nil || calls.Load() != 2 {
		t.Fatalf("error=%v calls=%d", err, calls.Load())
	}
	if err != nil && strings.Contains(err.Error(), "secret") {
		t.Fatal("token leaked in error")
	}
}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := NewClient(Options{BaseURL: baseURL, Token: "secret", Timeout: time.Second, MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	return client
}
