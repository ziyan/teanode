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
	"github.com/ziyan/teanode/internal/config"
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
				// A read that goes out to the web.
				Annotations: tools.OpenWorldRead(),
				Guidance:    "web_search and web_fetch look things up in the world; mail_search searches only the person's mail. A search finds pages; a price, an opening hour or a detail is on the page, so read the page rather than searching again for the detail in the snippets. Set freshness only when the question is about recent news: most pages worth reading have not changed this month.",
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

// Result is one page a search found. PictureURL is the picture the search
// service keeps for the page, when it has one: often a product page's own
// photo, which the page itself may refuse to a server that asks for it.
type Result struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Age         string `json:"age"`
	PictureURL  string `json:"pictureUrl,omitempty"`
}

// ImageResult is one picture an image search found: where it is, and the
// page it is on.
type ImageResult struct {
	Title      string `json:"title"`
	PageURL    string `json:"pageUrl"`
	PictureURL string `json:"pictureUrl"`
}

// Search asks the configured search service for pages about query: count of
// them (5 when zero, 20 at most), as fresh as freshness says (day, week,
// month, year, or anything when empty). The web_search tool and the server's
// own jobs (finding a shopping item's picture) both ask through it.
func Search(ctx context.Context, search config.AgentSearch, query string, count int, freshness string) ([]Result, error) {
	values := url.Values{}
	if freshness := map[string]string{"day": "pd", "week": "pw", "month": "pm", "year": "py"}[freshness]; freshness != "" {
		values.Set("freshness", freshness)
	}
	var decoded struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
				Age         string `json:"age"`
				Thumbnail   struct {
					Original string `json:"original"`
				} `json:"thumbnail"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := ask(ctx, search, "web", query, count, values, &decoded); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(decoded.Web.Results))
	for _, each := range decoded.Web.Results {
		results = append(results, Result{Title: each.Title, URL: each.URL, Description: each.Description, Age: each.Age, PictureURL: each.Thumbnail.Original})
	}
	return results, nil
}

// SearchImages asks the configured search service for pictures of query,
// count of them (5 when zero, 20 at most). A shopping item with no picture
// of its very product is shown a picture of the thing found this way.
func SearchImages(ctx context.Context, search config.AgentSearch, query string, count int) ([]ImageResult, error) {
	var decoded struct {
		Results []struct {
			Title      string `json:"title"`
			URL        string `json:"url"`
			Properties struct {
				URL string `json:"url"`
			} `json:"properties"`
		} `json:"results"`
	}
	if err := ask(ctx, search, "images", query, count, url.Values{}, &decoded); err != nil {
		return nil, err
	}
	results := make([]ImageResult, 0, len(decoded.Results))
	for _, each := range decoded.Results {
		if each.Properties.URL != "" {
			results = append(results, ImageResult{Title: each.Title, PageURL: each.URL, PictureURL: each.Properties.URL})
		}
	}
	return results, nil
}

// ask sends one search of kind (web or images) and decodes the answer into
// decoded.
func ask(ctx context.Context, search config.AgentSearch, kind, query string, count int, values url.Values, decoded any) error {
	if search.Kind == "" || search.APIKey == "" {
		return fmt.Errorf("no search service is configured on this server")
	}
	if search.Kind != "brave" {
		return fmt.Errorf("the search service %q is not supported", search.Kind)
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return fmt.Errorf("nothing to search for")
	}
	if count <= 0 {
		count = 5
	}
	if count > 20 {
		count = 20
	}
	values.Set("q", query)
	values.Set("count", strconv.Itoa(count))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.search.brave.com/res/v1/"+kind+"/search?"+values.Encode(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Subscription-Token", search.APIKey)
	client := &http.Client{Timeout: 20 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("the search service did not answer: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("the search service answered %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(decoded)
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
	query := strings.TrimSpace(arguments.Query)
	if query == "" {
		return nil, fmt.Errorf("nothing to search for")
	}
	if site := strings.TrimSpace(arguments.Site); site != "" {
		query += " site:" + site
	}
	found, err := Search(ctx, configuration.Agent.Search, query, arguments.Count, arguments.Freshness)
	if err != nil {
		return nil, err
	}
	decoded := struct{ Web struct{ Results []Result } }{}
	decoded.Web.Results = found
	results := make([]map[string]any, 0, len(decoded.Web.Results))
	for _, entry := range decoded.Web.Results {
		results = append(results, map[string]any{"title": entry.Title, "url": entry.URL, "description": entry.Description, "age": entry.Age})
	}
	result, err := tools.JSONResult(map[string]any{"query": query, "results": results})
	if err != nil {
		return nil, err
	}
	for _, entry := range decoded.Web.Results {
		result.SearchedAddresses = append(result.SearchedAddresses, entry.URL)
	}
	result.Untrusted = true
	result.Note = fmt.Sprintf("searched for %q", query)
	return result, nil
}
