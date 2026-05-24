package jenkins

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestParseJenkinsSuggestResponse_DoSuggestBean(t *testing.T) {
	body := []byte(`{"suggestions":[{"name":"team/foo/bar","icon":"sym"},{"name":"team/other","group":"Jobs"}]}`)
	out := parseJenkinsSuggestResponse("foo", "", body)
	if len(out.Suggestions) != 2 || out.Suggestions[0] != "team/foo/bar" || out.Suggestions[1] != "team/other" {
		t.Fatalf("suggestions=%#v", out.Suggestions)
	}
	if out.Query != "foo" {
		t.Fatalf("query=%q", out.Query)
	}
}

func TestParseJenkinsSuggestResponse_EmptySuggestionsKey(t *testing.T) {
	out := parseJenkinsSuggestResponse("z", "", []byte(`{"suggestions":[]}`))
	if out.RawJSON != "" || len(out.Suggestions) != 0 {
		t.Fatalf("%+v", out)
	}
}

func TestParseJenkinsSuggestResponse_OpenSearch(t *testing.T) {
	body := []byte(`["myq",["a/b","c/d"]]`)
	out := parseJenkinsSuggestResponse("myq", "scope", body)
	if out.Folder != "scope" || len(out.Suggestions) != 2 {
		t.Fatalf("%+v", out)
	}
}

func TestParseJenkinsSuggestResponse_StringArray(t *testing.T) {
	body := []byte(`["alpha"," beta "]`)
	out := parseJenkinsSuggestResponse("x", "", body)
	if len(out.Suggestions) != 2 || out.Suggestions[1] != "beta" {
		t.Fatalf("%+v", out)
	}
}

func TestParseJenkinsSuggestResponse_RawFallback(t *testing.T) {
	body := []byte(`not-json`)
	out := parseJenkinsSuggestResponse("x", "", body)
	if out.RawJSON != "not-json" || out.ParseNote == "" {
		t.Fatalf("%+v", out)
	}
}

func TestToolSearchJobs_RootPathAndAuth(t *testing.T) {
	var gotPath string
	var gotVals url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/suggest" {
			http.NotFound(w, r)
			return
		}
		u, p, ok := r.BasicAuth()
		if !ok || u != "svc" || p != "tok" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		gotPath = r.URL.Path
		gotVals = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"suggestions":[{"name":"job-a"}]}`))
	}))
	defer srv.Close()

	cfg := &JenkinsInstanceConfig{
		BaseURL:        strings.TrimSuffix(srv.URL, "/"),
		Username:       "svc",
		APIToken:       "tok",
		TimeoutSeconds: 5,
	}
	jc, err := newHTTPJenkinsClient(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	p := NewJenkinsPlugin()
	out, err := p.toolSearchJobs(map[string]any{"query": "job"}, jc)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/search/suggest" {
		t.Fatalf("path=%q", gotPath)
	}
	if gotVals.Get("query") != "job" {
		t.Fatalf("query param: %v", gotVals)
	}
	if len(out.Suggestions) != 1 || out.Suggestions[0] != "job-a" {
		t.Fatalf("%+v", out)
	}
}

func TestToolSearchJobs_FolderScoped(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if !strings.HasSuffix(r.URL.Path, "/search/suggest") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["q",["scoped/job"]]`))
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
	out, err := p.toolSearchJobs(map[string]any{"folder": "a/b", "query": "x"}, jc)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 1 || paths[len(paths)-1] != "/job/a/job/b/search/suggest" {
		t.Fatalf("paths=%v", paths)
	}
	if len(out.Suggestions) != 1 || out.Suggestions[0] != "scoped/job" {
		t.Fatalf("%+v", out)
	}
}

func TestSearchSuggest_NotFoundFallsBackToSuggestOpenSearch(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch {
		case r.URL.Path == "/search/suggest" && n == 1:
			http.NotFound(w, r)
		case r.URL.Path == "/search/suggestOpenSearch" && r.URL.Query().Get("q") == "hi":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`["hi",["p/q"]]`))
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
	data, err := jc.SearchSuggest(context.Background(), "", "hi")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("requests=%d want 2", n)
	}
	var pair []json.RawMessage
	if err := json.Unmarshal(data, &pair); err != nil {
		t.Fatal(err)
	}
}
