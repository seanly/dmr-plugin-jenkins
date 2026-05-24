package jenkins

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
	maxErrBodyLen  = 2048    // max bytes included in error message snippets (httpError)
	maxReadBodyLen = 8 << 20 // 8MiB
)

type crumbData struct {
	Crumb             string `json:"crumb"`
	CrumbRequestField string `json:"crumbRequestField"`
}

// httpJenkinsClient implements JenkinsClient using HTTP REST API
type httpJenkinsClient struct {
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

func newHTTPJenkinsClient(ctx context.Context, cfg *JenkinsInstanceConfig) (*httpJenkinsClient, error) {
	base, err := NormalizeBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}

	tlsConf := &tls.Config{
		InsecureSkipVerify: !cfg.normalizedVerifyTLS(),
	}
	baseTr := &http.Transport{TLSClientConfig: tlsConf}
	if s := strings.TrimSpace(cfg.HTTPProxy); s != "" {
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

	timeout := cfg.effectiveTimeout(nil)
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	return &httpJenkinsClient{
		baseURL: base,
		user:    strings.TrimSpace(cfg.Username),
		token:   strings.TrimSpace(cfg.APIToken),
		http: &http.Client{
			Transport: rt,
			Timeout:   timeout,
		},
	}, nil
}

func (jc *httpJenkinsClient) urlFor(path string) (string, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u, err := url.Parse(jc.baseURL + path)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (jc *httpJenkinsClient) doReq(ctx context.Context, method, path string, headers map[string]string, body io.Reader, contentType string) (int, []byte, error) {
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

func (jc *httpJenkinsClient) get(ctx context.Context, path string) (int, []byte, error) {
	return jc.doReq(ctx, http.MethodGet, path, nil, nil, "")
}

func (jc *httpJenkinsClient) clearCrumbLocked() {
	jc.crumbcache = nil
}

func (jc *httpJenkinsClient) fetchCrumb(ctx context.Context) (*crumbData, int, error) {
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

func (jc *httpJenkinsClient) applyCrumb(ctx context.Context, reqHeaders map[string]string) error {
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

func (jc *httpJenkinsClient) post(ctx context.Context, path string, contentType string, body []byte) (int, []byte, error) {
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

// JenkinsClient interface implementations

func (jc *httpJenkinsClient) GetJob(ctx context.Context, job string, tree string) ([]byte, error) {
	var path string
	if jenkinsGetJobIsRootListing(job) {
		if tree == "" {
			tree = defaultRootJobTree
		}
		path = "/api/json?tree=" + url.QueryEscape(tree)
	} else {
		if tree == "" {
			tree = defaultJobTree
		}
		path = jobURLPath(job) + "/api/json?tree=" + url.QueryEscape(tree)
	}

	st, body, err := jc.get(ctx, path)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, httpError(st, body)
	}
	return body, nil
}

func (jc *httpJenkinsClient) ListBuilds(ctx context.Context, job string, limit int) ([]byte, error) {
	if job == "" {
		return nil, fmt.Errorf("job is required for ListBuilds")
	}
	tree := fmt.Sprintf("builds[number,url,result,timestamp,building]{0,%d}", limit)
	path := jobURLPath(job) + "/api/json?tree=" + url.QueryEscape(tree)

	st, body, err := jc.get(ctx, path)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, httpError(st, body)
	}
	return body, nil
}

func (jc *httpJenkinsClient) GetBuild(ctx context.Context, job string, buildNumber int) ([]byte, error) {
	if job == "" {
		return nil, fmt.Errorf("job is required")
	}
	if buildNumber <= 0 {
		return nil, fmt.Errorf("build_number must be positive")
	}
	path := fmt.Sprintf("%s/%d/api/json", jobURLPath(job), buildNumber)

	st, body, err := jc.get(ctx, path)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, httpError(st, body)
	}
	return body, nil
}

func (jc *httpJenkinsClient) TriggerBuild(ctx context.Context, job string, params map[string]string) error {
	if job == "" {
		return fmt.Errorf("job is required")
	}
	base := jobURLPath(job)

	if len(params) == 0 {
		st, body, err := jc.post(ctx, base+"/build", "", nil)
		if err != nil {
			return err
		}
		if st != httpStatusCreated && st != 200 && st != 204 {
			return httpError(st, body)
		}
		return nil
	}

	vals := url.Values{}
	for k, v := range params {
		vals.Set(k, v)
	}
	st, body, err := jc.post(ctx, base+"/buildWithParameters", "application/x-www-form-urlencoded", []byte(vals.Encode()))
	if err != nil {
		return err
	}
	if st != httpStatusCreated && st != 200 && st != 204 {
		return httpError(st, body)
	}
	return nil
}

func (jc *httpJenkinsClient) GetConsoleText(ctx context.Context, job string, buildNumber int) (string, error) {
	if job == "" {
		return "", fmt.Errorf("job is required")
	}
	if buildNumber <= 0 {
		return "", fmt.Errorf("build_number must be positive")
	}
	path := jobConsolePath(job, buildNumber)

	st, body, err := jc.get(ctx, path)
	if err != nil {
		return "", err
	}
	if st != 200 {
		return "", httpError(st, body)
	}
	return string(body), nil
}

func (jc *httpJenkinsClient) GetComputers(ctx context.Context) ([]byte, error) {
	tree := "computer[displayName,executors[currentExecutable[url,number,fullDisplayName]]]"
	path := "/computer/api/json?tree=" + url.QueryEscape(tree)

	st, body, err := jc.get(ctx, path)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, httpError(st, body)
	}
	return body, nil
}

func (jc *httpJenkinsClient) GetQueue(ctx context.Context) ([]byte, error) {
	tree := "items[id,why,stuck,inQueueSince,task[name,url]]"
	path := "/queue/api/json?tree=" + url.QueryEscape(tree)

	st, body, err := jc.get(ctx, path)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, httpError(st, body)
	}
	return body, nil
}

func (jc *httpJenkinsClient) SearchSuggest(ctx context.Context, folder string, query string) ([]byte, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, fmt.Errorf("query is required")
	}
	f := strings.TrimSpace(folder)
	var base string
	if f == "" {
		base = "/search/suggest"
	} else {
		base = jobURLPath(f) + "/search/suggest"
	}
	path := base + "?query=" + url.QueryEscape(q)
	st, body, err := jc.get(ctx, path)
	if err != nil {
		return nil, err
	}
	if st == http.StatusNotFound {
		// Stapler name for Search#doSuggestOpenSearch (OpenSearch-style JSON payload).
		alt := strings.TrimSuffix(base, "/search/suggest") + "/search/suggestOpenSearch?q=" + url.QueryEscape(q)
		st2, body2, err2 := jc.get(ctx, alt)
		if err2 != nil {
			return nil, err2
		}
		if st2 != 200 {
			return nil, httpError(st2, body2)
		}
		return body2, nil
	}
	if st != 200 {
		return nil, httpError(st, body)
	}
	return body, nil
}

func (jc *httpJenkinsClient) Close() error {
	// Nothing to close for HTTP client
	return nil
}
