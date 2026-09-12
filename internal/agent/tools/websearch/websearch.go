// Package websearch asks the search service the operator configured.
package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name:        "web_search",
				Family:      tools.FamilyGeneral,
				Core:        true,
				Risk:        tools.RiskRead,
				Description: "Search the web. Returns titles, addresses and a line each. Only offered when the operator configured a search service.",
				Parameters: tools.Object(map[string]any{
					"query":     tools.StringProperty("the search"),
					"count":     tools.IntegerProperty("how many results, 5 by default, 20 at most"),
					"freshness": tools.EnumProperty("how recent the results must be", "day", "week", "month", "year"),
					"site":      tools.StringProperty("limit to one site"),
				}, "query"),
				Run: runWebSearch,
			},
		}
	})
}

type webSearchArguments struct {
	Query     string `json:"query"`
	Count     int    `json:"count"`
	Freshness string `json:"freshness"`
	Site      string `json:"site"`
}

func runWebSearch(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[webSearchArguments](call)
	if err != nil {
		return nil, err
	}
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	configuration := run.Configuration()
	search := configuration.Agent.Search
	if search.Kind == "" || search.APIKey == "" {
		return nil, fmt.Errorf("no search service is configured on this server")
	}
	if search.Kind != "brave" {
		return nil, fmt.Errorf("the search service %q is not supported", search.Kind)
	}
	query := strings.TrimSpace(arguments.Query)
	if query == "" {
		return nil, fmt.Errorf("nothing to search for")
	}
	if site := strings.TrimSpace(arguments.Site); site != "" {
		query += " site:" + site
	}
	count := arguments.Count
	if count <= 0 {
		count = 5
	}
	if count > 20 {
		count = 20
	}
	values := url.Values{"q": {query}, "count": {strconv.Itoa(count)}}
	if freshness := map[string]string{"day": "pd", "week": "pw", "month": "pm", "year": "py"}[arguments.Freshness]; freshness != "" {
		values.Set("freshness", freshness)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.search.brave.com/res/v1/web/search?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Subscription-Token", search.APIKey)
	client := &http.Client{Timeout: 20 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("the search service did not answer: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the search service answered %d", response.StatusCode)
	}
	var decoded struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
				Age         string `json:"age"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&decoded); err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(decoded.Web.Results))
	for _, entry := range decoded.Web.Results {
		results = append(results, map[string]any{"title": entry.Title, "url": entry.URL, "description": entry.Description, "age": entry.Age})
	}
	result, err := tools.JSONResult(map[string]any{"query": query, "results": results})
	if err != nil {
		return nil, err
	}
	result.Untrusted = true
	result.Note = fmt.Sprintf("searched for %q", query)
	return result, nil
}
