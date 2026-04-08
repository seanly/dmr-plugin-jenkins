package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestJenkinsClientCrumbAndPost(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/crumbIssuer/api/json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"crumb":             "abc123",
				"crumbRequestField": "Jenkins-Crumb",
			})
		case r.URL.Path == "/job/x/build" && r.Method == http.MethodPost:
			posts.Add(1)
			if r.Header.Get("Jenkins-Crumb") != "abc123" {
				http.Error(w, "missing crumb", http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusCreated)
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
	st, _, err := jc.post(context.Background(), "/job/x/build", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if st != http.StatusCreated {
		t.Fatalf("status %d", st)
	}
	if posts.Load() != 1 {
		t.Fatalf("posts = %d", posts.Load())
	}
}

func TestJenkinsClientNoCrumb404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/crumbIssuer/api/json" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/job/x/build" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			return
		}
		http.NotFound(w, r)
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
	st, _, err := jc.post(context.Background(), "/job/x/build", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if st != http.StatusCreated {
		t.Fatalf("status %d", st)
	}
}

func TestTruncateBody(t *testing.T) {
	b := make([]byte, maxErrBodyLen+10)
	for i := range b {
		b[i] = 'x'
	}
	s := truncateBody(b)
	if len(s) <= maxErrBodyLen {
		t.Fatalf("expected truncation")
	}
	if !strings.HasSuffix(s, "...") {
		t.Fatalf("expected ellipsis")
	}
}

func TestDoReqReadsFullResponseWithinCap(t *testing.T) {
	const n = 100_000 // well above old 2KiB accidental cap, well below maxReadBodyLen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = io.WriteString(w, strings.Repeat("a", n))
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
	st, body, err := jc.get(context.Background(), "/big")
	if err != nil {
		t.Fatal(err)
	}
	if st != 200 {
		t.Fatalf("status %d", st)
	}
	if len(body) != n {
		t.Fatalf("len %d; want %d (response must not be capped at maxErrBodyLen)", len(body), n)
	}
}

func TestReadHTTPBody_RejectsOverMax(t *testing.T) {
	const max = 100
	r := strings.NewReader(strings.Repeat("b", max+50))
	_, err := readHTTPBody(r, max)
	if err == nil {
		t.Fatal("expected error when body exceeds max")
	}
}

func TestReadHTTPBody_OkAtExactlyMax(t *testing.T) {
	const max = 50
	want := strings.Repeat("c", max)
	b, err := readHTTPBody(strings.NewReader(want), max)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != want {
		t.Fatalf("got len %d", len(b))
	}
}
