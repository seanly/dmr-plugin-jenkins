package jenkins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestToolListBuilds_GlobalInProgress(t *testing.T) {
	var gotComputerTree string
	var gotQueueTree string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/computer/api/json":
			gotComputerTree = r.URL.Query().Get("tree")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"computer": [
					{
						"displayName": "nodeA",
						"executors": [
							{
								"currentExecutable": {
									"url": "/job/team/job/build/123/",
									"number": 123,
									"fullDisplayName": "team/build #123"
								}
							}
						]
					}
				]
			}`))
		case "/queue/api/json":
			gotQueueTree = r.URL.Query().Get("tree")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"items": [
					{
						"id": 7,
						"why": "Waiting for executor",
						"stuck": false,
						"inQueueSince": 1700000000000,
						"task": {
							"name": "team/build",
							"url": "/job/team/job/build/"
						}
					}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &JenkinsInstanceConfig{
		BaseURL:        strings.TrimSuffix(srv.URL, "/"),
		Username:       "u",
		APIToken:       "t",
		TimeoutSeconds: 5,
	}
	jc, err := newHTTPJenkinsClient(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	p := NewJenkinsPlugin()
	out, err := p.toolListBuilds(map[string]any{}, jc)
	if err != nil {
		t.Fatal(err)
	}

	expectedComputerTree := "computer[displayName,executors[currentExecutable[url,number,fullDisplayName]]]"
	if gotComputerTree != expectedComputerTree {
		t.Fatalf("computer tree = %q; want %q", gotComputerTree, expectedComputerTree)
	}
	expectedQueueTree := "items[id,why,stuck,inQueueSince,task[name,url]]"
	if gotQueueTree != expectedQueueTree {
		t.Fatalf("queue tree = %q; want %q", gotQueueTree, expectedQueueTree)
	}

	globalOut, ok := out.(*ListBuildsGlobalModeResponse)
	if !ok {
		t.Fatalf("expected *ListBuildsGlobalModeResponse, got %T", out)
	}

	if len(globalOut.Running) != 1 {
		t.Fatalf("running len = %d; want 1", len(globalOut.Running))
	}
	if globalOut.Running[0].Job != "team/build" {
		t.Fatalf("running job = %v; want team/build", globalOut.Running[0].Job)
	}
	if globalOut.Running[0].BuildNumber != 123 {
		t.Fatalf("build_number = %v; want 123", globalOut.Running[0].BuildNumber)
	}
	if globalOut.Running[0].Node != "nodeA" {
		t.Fatalf("node = %v; want nodeA", globalOut.Running[0].Node)
	}

	if len(globalOut.Queued) != 1 {
		t.Fatalf("queued len = %d; want 1", len(globalOut.Queued))
	}
	if globalOut.Queued[0].Job != "team/build" {
		t.Fatalf("queued job = %v; want team/build", globalOut.Queued[0].Job)
	}
	if globalOut.Queued[0].QueueID != 7 {
		t.Fatalf("queue_id = %v; want 7", globalOut.Queued[0].QueueID)
	}
	if globalOut.Queued[0].InQueueSince != 1700000000000 {
		t.Fatalf("in_queue_since = %v; want 1700000000000", globalOut.Queued[0].InQueueSince)
	}
}

func TestToolListBuilds_GlobalInProgress_IncludeFlags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/computer/api/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"computer": []}`))
		case "/queue/api/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items": []}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &JenkinsInstanceConfig{
		BaseURL:        strings.TrimSuffix(srv.URL, "/"),
		Username:       "u",
		APIToken:       "t",
		TimeoutSeconds: 5,
	}
	jc, err := newHTTPJenkinsClient(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	p := NewJenkinsPlugin()
	out, err := p.toolListBuilds(map[string]any{
		"include_running": false,
		"include_queued":  true,
	}, jc)
	if err != nil {
		t.Fatal(err)
	}

	globalOut, ok := out.(*ListBuildsGlobalModeResponse)
	if !ok {
		t.Fatalf("expected *ListBuildsGlobalModeResponse, got %T", out)
	}

	if len(globalOut.Running) != 0 {
		t.Fatalf("running len = %d; want 0", len(globalOut.Running))
	}
}

func TestToolListBuilds_PartialComputerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/computer/api/json":
			http.Error(w, "boom", http.StatusInternalServerError)
		case "/queue/api/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"items": [
					{
						"id": 1,
						"why": "waiting",
						"stuck": false,
						"inQueueSince": 0,
						"task": {
							"name": "leaf",
							"url": "/job/folder/job/leaf/"
						}
					}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &JenkinsInstanceConfig{
		BaseURL:        strings.TrimSuffix(srv.URL, "/"),
		Username:       "u",
		APIToken:       "t",
		TimeoutSeconds: 5,
	}
	jc, err := newHTTPJenkinsClient(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	p := NewJenkinsPlugin()
	out, err := p.toolListBuilds(map[string]any{}, jc)
	if err != nil {
		t.Fatal(err)
	}

	globalOut, ok := out.(*ListBuildsGlobalModeResponse)
	if !ok {
		t.Fatalf("expected *ListBuildsGlobalModeResponse, got %T", out)
	}

	if globalOut.Errors == nil || !strings.Contains(globalOut.Errors["computer"], "500") {
		t.Fatalf("expected computer error mentioning status; got %q", globalOut.Errors["computer"])
	}
	if len(globalOut.Queued) != 1 {
		t.Fatalf("queued len = %d; want 1", len(globalOut.Queued))
	}
	if globalOut.Queued[0].Job != "folder/leaf" {
		t.Fatalf("job = %v", globalOut.Queued[0].Job)
	}
}

func TestToolListBuilds_UnparsedRunningUsesDisplayName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/computer/api/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"computer": [
					{
						"displayName": "n1",
						"executors": [
							{
								"currentExecutable": {
									"url": "/not/a/jenkins/style",
									"number": 0,
									"fullDisplayName": "team » ci » myjob #77"
								}
							}
						]
					}
				]
			}`))
		case "/queue/api/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items": []}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &JenkinsInstanceConfig{
		BaseURL:        strings.TrimSuffix(srv.URL, "/"),
		Username:       "u",
		APIToken:       "t",
		TimeoutSeconds: 5,
	}
	jc, err := newHTTPJenkinsClient(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	p := NewJenkinsPlugin()
	out, err := p.toolListBuilds(map[string]any{}, jc)
	if err != nil {
		t.Fatal(err)
	}

	globalOut, ok := out.(*ListBuildsGlobalModeResponse)
	if !ok {
		t.Fatalf("expected *ListBuildsGlobalModeResponse, got %T", out)
	}

	if len(globalOut.Running) != 1 {
		t.Fatalf("running len = %d", len(globalOut.Running))
	}
	if globalOut.Running[0].Job != "team/ci/myjob" {
		t.Fatalf("job = %v", globalOut.Running[0].Job)
	}
	if globalOut.Running[0].BuildNumber != 77 {
		t.Fatalf("build_number = %v", globalOut.Running[0].BuildNumber)
	}
	if globalOut.Running[0].Unparsed {
		t.Fatal("expected parsed from fullDisplayName")
	}
}

func TestToolListBuilds_UnparsedFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/computer/api/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"computer": [
					{
						"displayName": "n1",
						"executors": [
							{
								"currentExecutable": {
									"url": "?",
									"number": 15,
									"fullDisplayName": ""
								}
							}
						]
					}
				]
			}`))
		case "/queue/api/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items": []}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &JenkinsInstanceConfig{
		BaseURL:        strings.TrimSuffix(srv.URL, "/"),
		Username:       "u",
		APIToken:       "t",
		TimeoutSeconds: 5,
	}
	jc, err := newHTTPJenkinsClient(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	p := NewJenkinsPlugin()
	out, err := p.toolListBuilds(map[string]any{}, jc)
	if err != nil {
		t.Fatal(err)
	}

	globalOut, ok := out.(*ListBuildsGlobalModeResponse)
	if !ok {
		t.Fatalf("expected *ListBuildsGlobalModeResponse, got %T", out)
	}

	if !globalOut.Running[0].Unparsed {
		t.Fatal("expected unparsed")
	}
	if globalOut.Running[0].BuildNumber != 15 {
		t.Fatalf("build_number = %v", globalOut.Running[0].BuildNumber)
	}
}
