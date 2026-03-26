package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	maxErrBodyLen = 2048 // max bytes included in error message snippets (httpError)
	// maxReadBodyLen caps how much of any response body we read (protects memory on malicious huge bodies).
	// Jenkins /computer and /queue JSON can exceed a few KB; the previous 2KiB cap truncated valid JSON and broke json.Unmarshal.
	maxReadBodyLen = 8 << 20 // 8MiB
)

type crumbData struct {
	Crumb             string `json:"crumb"`
	CrumbRequestField string `json:"crumbRequestField"`
}

type jenkinsClient struct {
	baseURL string
	user    string
	token   string
	http    *http.Client

	mu          sync.Mutex
	crumbcache  *crumbData
	crumbbuster bool
}

type jarRoundTripper struct {
	rt  http.RoundTripper
	jar http.CookieJar
}

func (j *jarRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if j.jar != nil {
		for _, c := range j.jar.Cookies(req.URL) {
			req.AddCookie(c)
		}
	}
	resp, err := j.rt.RoundTrip(req)
	if err != nil || j.jar == nil || resp == nil {
		return resp, err
	}
	if cookies := resp.Cookies(); len(cookies) > 0 {
		j.jar.SetCookies(req.URL, cookies)
	}
	return resp, err
}

func newJenkinsClient(base, user, token string, verifyTLS bool, timeout time.Duration, proxyURL string) (*jenkinsClient, error) {
	tlsConf := &tls.Config{ //nolint:gosec // InsecureSkipVerify is user-configurable for private CAs
		InsecureSkipVerify: !verifyTLS,
	}
	baseTr := &http.Transport{TLSClientConfig: tlsConf}
	if s := strings.TrimSpace(proxyURL); s != "" {
		u, err := url.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("http_proxy: %w", err)
		}
		baseTr.Proxy = http.ProxyURL(u)
	} else {
		baseTr.Proxy = http.ProxyFromEnvironment
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	rt := &jarRoundTripper{rt: baseTr, jar: jar}

	return &jenkinsClient{
		baseURL: base,
		user:    user,
		token:   token,
		http: &http.Client{
			Transport: rt,
			Timeout:   timeout,
		},
	}, nil
}

func (jc *jenkinsClient) urlFor(path string) (string, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u, err := url.Parse(jc.baseURL + path)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (jc *jenkinsClient) doReq(ctx context.Context, method, path string, headers map[string]string, body io.Reader, contentType string) (int, []byte, error) {
	reqURL, err := jc.urlFor(path)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return 0, nil, err
	}
	req.SetBasicAuth(jc.user, jc.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := jc.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, err := readHTTPBody(resp.Body, maxReadBodyLen)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, b, nil
}

// readHTTPBody reads up to maxBytes from r. If more than maxBytes are available,
// returns an error so callers never decode truncated JSON.
func readHTTPBody(r io.Reader, maxBytes int) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBytes {
		return nil, fmt.Errorf("jenkins: response body exceeds %d bytes (maxReadBodyLen); narrow the API tree or raise the limit in plugin", maxBytes)
	}
	return b, nil
}

func (jc *jenkinsClient) get(ctx context.Context, path string) (int, []byte, error) {
	return jc.doReq(ctx, http.MethodGet, path, nil, nil, "")
}

func (jc *jenkinsClient) clearCrumbLocked() {
	jc.crumbcache = nil
}

func (jc *jenkinsClient) fetchCrumb(ctx context.Context) (*crumbData, int, error) {
	status, body, err := jc.get(ctx, "/crumbIssuer/api/json")
	if err != nil {
		return nil, status, err
	}
	if status == http.StatusNotFound {
		return nil, status, nil
	}
	if status != http.StatusOK {
		return nil, status, fmt.Errorf("crumbIssuer status %d: %s", status, truncateBody(body))
	}
	var c crumbData
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, status, fmt.Errorf("crumb JSON: %w", err)
	}
	if c.Crumb == "" || c.CrumbRequestField == "" {
		return nil, status, fmt.Errorf("crumb response missing fields")
	}
	return &c, status, nil
}

func (jc *jenkinsClient) applyCrumb(ctx context.Context, reqHeaders map[string]string) error {
	jc.mu.Lock()
	defer jc.mu.Unlock()

	if jc.crumbbuster {
		jc.crumbbuster = false
		jc.clearCrumbLocked()
	}

	if jc.crumbcache != nil {
		reqHeaders[jc.crumbcache.CrumbRequestField] = jc.crumbcache.Crumb
		return nil
	}

	c, status, err := jc.fetchCrumb(ctx)
	if err != nil && status != http.StatusNotFound {
		return err
	}
	if c != nil {
		jc.crumbcache = c
		reqHeaders[c.CrumbRequestField] = c.Crumb
	}
	return nil
}

func (jc *jenkinsClient) post(ctx context.Context, path string, contentType string, body []byte) (int, []byte, error) {
	var r io.Reader
	if len(body) > 0 {
		r = bytes.NewReader(body)
	}
	headers := make(map[string]string)
	if err := jc.applyCrumb(ctx, headers); err != nil {
		return 0, nil, fmt.Errorf("crumb: %w", err)
	}
	status, respBody, err := jc.doReq(ctx, http.MethodPost, path, headers, r, contentType)
	if err != nil {
		return status, respBody, err
	}
	if status == http.StatusForbidden && looksLikeInvalidCrumb(respBody) {
		jc.mu.Lock()
		jc.clearCrumbLocked()
		jc.crumbbuster = true
		jc.mu.Unlock()

		headers = make(map[string]string)
		if err := jc.applyCrumb(ctx, headers); err != nil {
			return status, respBody, err
		}
		r = nil
		if len(body) > 0 {
			r = bytes.NewReader(body)
		}
		status, respBody, err = jc.doReq(ctx, http.MethodPost, path, headers, r, contentType)
	}
	return status, respBody, err
}

func looksLikeInvalidCrumb(body []byte) bool {
	s := strings.ToLower(string(body))
	return strings.Contains(s, "crumb") && (strings.Contains(s, "invalid") || strings.Contains(s, "no valid"))
}

func truncateBody(b []byte) string {
	if len(b) <= maxErrBodyLen {
		return string(b)
	}
	return string(b[:maxErrBodyLen]) + "..."
}

func httpError(status int, body []byte) error {
	return fmt.Errorf("jenkins: HTTP %d: %s", status, truncateBody(body))
}
