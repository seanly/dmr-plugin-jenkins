package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

	jc, err := newJenkinsClient(strings.TrimSuffix(srv.URL, "/"), "u", "t", true, 5*time.Second, "")
	if err != nil {
		t.Fatal(err)
	}

	p := NewJenkinsPlugin()
	out, err := p.toolListBuilds(context.Background(), jc, map[string]any{})
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

	outMap := out.(map[string]any)

	runningArr := outMap["running"].([]map[string]any)
	if len(runningArr) != 1 {
		t.Fatalf("running len = %d; want 1", len(runningArr))
	}
	if runningArr[0]["job"].(string) != "team/build" {
		t.Fatalf("running job = %v; want team/build", runningArr[0]["job"])
	}
	if runningArr[0]["build_number"].(int) != 123 {
		t.Fatalf("build_number = %v; want 123", runningArr[0]["build_number"])
	}
	if runningArr[0]["node"].(string) != "nodeA" {
		t.Fatalf("node = %v; want nodeA", runningArr[0]["node"])
	}

	queuedArr := outMap["queued"].([]map[string]any)
	if len(queuedArr) != 1 {
		t.Fatalf("queued len = %d; want 1", len(queuedArr))
	}
	if queuedArr[0]["job"].(string) != "team/build" {
		t.Fatalf("queued job = %v; want team/build", queuedArr[0]["job"])
	}
	if queuedArr[0]["queue_id"].(int) != 7 {
		t.Fatalf("queue_id = %v; want 7", queuedArr[0]["queue_id"])
	}
	if queuedArr[0]["in_queue_since"].(int64) != 1700000000000 {
		t.Fatalf("in_queue_since = %v; want 1700000000000", queuedArr[0]["in_queue_since"])
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

	jc, err := newJenkinsClient(strings.TrimSuffix(srv.URL, "/"), "u", "t", true, 5*time.Second, "")
	if err != nil {
		t.Fatal(err)
	}

	p := NewJenkinsPlugin()
	out, err := p.toolListBuilds(context.Background(), jc, map[string]any{
		"include_running": false,
		"include_queued":  true,
	})
	if err != nil {
		t.Fatal(err)
	}

	outMap := out.(map[string]any)
	runningArr := outMap["running"].([]map[string]any)
	if len(runningArr) != 0 {
		t.Fatalf("running len = %d; want 0", len(runningArr))
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

	jc, err := newJenkinsClient(strings.TrimSuffix(srv.URL, "/"), "u", "t", true, 5*time.Second, "")
	if err != nil {
		t.Fatal(err)
	}

	p := NewJenkinsPlugin()
	out, err := p.toolListBuilds(context.Background(), jc, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	outMap := out.(map[string]any)
	errs := outMap["errors"].(map[string]string)
	if !strings.Contains(errs["computer"], "500") {
		t.Fatalf("expected computer error mentioning status; got %q", errs["computer"])
	}
	queuedArr := outMap["queued"].([]map[string]any)
	if len(queuedArr) != 1 {
		t.Fatalf("queued len = %d; want 1", len(queuedArr))
	}
	if queuedArr[0]["job"].(string) != "folder/leaf" {
		t.Fatalf("job = %v", queuedArr[0]["job"])
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

	jc, err := newJenkinsClient(strings.TrimSuffix(srv.URL, "/"), "u", "t", true, 5*time.Second, "")
	if err != nil {
		t.Fatal(err)
	}

	p := NewJenkinsPlugin()
	out, err := p.toolListBuilds(context.Background(), jc, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	outMap := out.(map[string]any)
	runningArr := outMap["running"].([]map[string]any)
	if len(runningArr) != 1 {
		t.Fatalf("running len = %d", len(runningArr))
	}
	if runningArr[0]["job"].(string) != "team/ci/myjob" {
		t.Fatalf("job = %v", runningArr[0]["job"])
	}
	if runningArr[0]["build_number"].(int) != 77 {
		t.Fatalf("build_number = %v", runningArr[0]["build_number"])
	}
	if _, bad := runningArr[0]["unparsed"]; bad {
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

	jc, err := newJenkinsClient(strings.TrimSuffix(srv.URL, "/"), "u", "t", true, 5*time.Second, "")
	if err != nil {
		t.Fatal(err)
	}

	p := NewJenkinsPlugin()
	out, err := p.toolListBuilds(context.Background(), jc, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	outMap := out.(map[string]any)
	runningArr := outMap["running"].([]map[string]any)
	if !runningArr[0]["unparsed"].(bool) {
		t.Fatal("expected unparsed")
	}
	if runningArr[0]["build_number"].(int) != 15 {
		t.Fatalf("build_number = %v", runningArr[0]["build_number"])
	}
}
