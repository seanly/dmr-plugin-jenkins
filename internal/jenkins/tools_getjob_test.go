package jenkins

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestToolGetJob_RootListing_EmptyJob(t *testing.T) {
	var gotPath, gotTree string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTree = r.URL.Query().Get("tree")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jobs":[{"name":"alpha","url":"http://x/job/alpha/"}]}`))
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
	out, err := p.toolGetJob(map[string]any{}, jc)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/json" {
		t.Fatalf("path = %q; want /api/json", gotPath)
	}
	if gotTree != defaultRootJobTree {
		t.Fatalf("tree = %q; want %q", gotTree, defaultRootJobTree)
	}
	var wrap struct {
		Jobs []struct {
			Name string `json:"name"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(out.Data, &wrap); err != nil {
		t.Fatal(err)
	}
	if len(wrap.Jobs) != 1 || wrap.Jobs[0].Name != "alpha" {
		t.Fatalf("jobs = %+v", wrap.Jobs)
	}
}

func TestToolGetJob_RootListing_DotJob(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jobs":[]}`))
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
	_, err = p.toolGetJob(map[string]any{"job": "."}, jc)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/json" {
		t.Fatalf("path = %q; want /api/json", gotPath)
	}
}

func TestToolGetJob_NamedJobUsesJobPath(t *testing.T) {
	var gotPath, gotTree string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTree = r.URL.Query().Get("tree")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"leaf","buildable":true}`))
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
	_, err = p.toolGetJob(map[string]any{"job": "folder/leaf"}, jc)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/job/folder/job/leaf/api/json" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotTree != defaultJobTree {
		t.Fatalf("tree = %q; want default job tree", gotTree)
	}
}
