package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (p *JenkinsPlugin) toolSearchJobs(args map[string]any, client JenkinsClient) (*SearchJobsResponse, error) {
	parser := NewRequestParser(args)
	folder := parser.String("folder")
	query := strings.TrimSpace(parser.String("query"))
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	body, err := client.SearchSuggest(context.Background(), folder, query)
	if err != nil {
		return nil, err
	}
	return parseJenkinsSuggestResponse(query, folder, body), nil
}

// parseJenkinsSuggestResponse parses Jenkins Search#doSuggest (JSON bean) or
// doSuggestOpenSearch (["query",[paths...]]) payloads into Suggestions.
func parseJenkinsSuggestResponse(query string, folder string, body []byte) *SearchJobsResponse {
	out := &SearchJobsResponse{
		Query:  query,
		Folder: folder,
	}

	// 1) Standard doSuggest JSON: {"suggestions":[{"name":"path",...}]}
	var rawObj map[string]json.RawMessage
	if err := json.Unmarshal(body, &rawObj); err == nil {
		if rawSug, ok := rawObj["suggestions"]; ok {
			var items []struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(rawSug, &items); err == nil {
				for _, s := range items {
					n := strings.TrimSpace(s.Name)
					if n != "" {
						out.Suggestions = append(out.Suggestions, n)
					}
				}
				return out
			}
		}
	}

	// 2) OpenSearch tuple: ["queryString", ["comp1", "comp2", ...]]
	var top []json.RawMessage
	if err := json.Unmarshal(body, &top); err == nil && len(top) >= 2 {
		var comps []string
		if json.Unmarshal(top[1], &comps) == nil && len(comps) > 0 {
			for _, s := range comps {
				s = strings.TrimSpace(s)
				if s != "" {
					out.Suggestions = append(out.Suggestions, s)
				}
			}
			return out
		}
	}

	// 3) Plain JSON array of strings
	var strs []string
	if err := json.Unmarshal(body, &strs); err == nil && len(strs) > 0 {
		for _, s := range strs {
			s = strings.TrimSpace(s)
			if s != "" {
				out.Suggestions = append(out.Suggestions, s)
			}
		}
		return out
	}

	// 4) Array of loose values (numbers, strings, objects)
	var generic []interface{}
	if err := json.Unmarshal(body, &generic); err == nil && len(generic) > 0 {
		for _, v := range generic {
			s := suggestItemToString(v)
			if s != "" {
				out.Suggestions = append(out.Suggestions, s)
			}
		}
		if len(out.Suggestions) > 0 {
			return out
		}
	}

	out.RawJSON = string(body)
	out.ParseNote = "unrecognized suggest JSON shape; inspect raw_json"
	return out
}

func suggestItemToString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return fmt.Sprintf("%.0f", t)
	case bool:
		return fmt.Sprintf("%v", t)
	case map[string]interface{}:
		for _, key := range []string{"name", "displayName", "path", "url"} {
			if s, ok := t[key].(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					return s
				}
			}
		}
	}
	return ""
}
