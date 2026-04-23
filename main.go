package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultTimeout = 30 * time.Second

type cubeClient struct {
	apiURL string
	client *http.Client
}

func newCubeClient(apiURL string) *cubeClient {
	return &cubeClient{
		apiURL: strings.TrimRight(apiURL, "/"),
		client: &http.Client{Timeout: defaultTimeout},
	}
}

func (c *cubeClient) doRequest(req *http.Request, auth string) (*mcp.CallToolResult, error) {
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cube API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{
				Text: fmt.Sprintf("Cube API error (HTTP %d): %s", resp.StatusCode, body),
			}},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil
}

type metaParams struct{}

func (c *cubeClient) meta(ctx context.Context, auth string) (*mcp.CallToolResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL+"/v1/meta", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	return c.doRequest(req, auth)
}

type queryFilter struct {
	Member   string   `json:"member" jsonschema:"The dimension or measure to filter on (e.g. \"Orders.status\")."`
	Operator string   `json:"operator" jsonschema:"Filter operator. One of: equals, notEquals, contains, notContains, startsWith, endsWith, gt, gte, lt, lte, set, notSet, inDateRange, notInDateRange, beforeDate, beforeOrOnDate, afterDate, afterOrOnDate."`
	Values   []string `json:"values,omitempty" jsonschema:"Values for the filter. Omit for set/notSet."`
}

type timeDimension struct {
	Dimension   string `json:"dimension" jsonschema:"The time dimension (e.g. \"Orders.createdAt\")."`
	DateRange   any    `json:"dateRange,omitempty" jsonschema:"Date range: a named range string (e.g. \"last 7 days\"), a single ISO date, or a [start, end] array of two ISO dates."`
	Granularity string `json:"granularity,omitempty" jsonschema:"Granularity. One of: second, minute, hour, day, week, month, quarter, year."`
}

type queryParams struct {
	Measures       []string        `json:"measures,omitempty" jsonschema:"Aggregate measures to compute (e.g. [\"Orders.count\", \"Orders.totalAmount\"])."`
	Dimensions     []string        `json:"dimensions,omitempty" jsonschema:"Dimensions to group by (e.g. [\"Orders.status\", \"Products.category\"])."`
	Filters        []queryFilter   `json:"filters,omitempty" jsonschema:"Filters to apply."`
	TimeDimensions []timeDimension `json:"timeDimensions,omitempty" jsonschema:"Time-based dimensions with optional date ranges and granularity."`
	Limit          *int            `json:"limit,omitempty" jsonschema:"Maximum number of rows to return."`
	Offset         *int            `json:"offset,omitempty" jsonschema:"Number of rows to skip."`
	Order          json.RawMessage `json:"order,omitempty" jsonschema:"Sort order as an object mapping member names to \"asc\" or \"desc\"."`
	Timezone       string          `json:"timezone,omitempty" jsonschema:"Timezone for time dimension calculations (e.g. \"America/New_York\")."`
}

func (c *cubeClient) query(ctx context.Context, auth string, params queryParams) (*mcp.CallToolResult, error) {
	body, err := json.Marshal(map[string]any{"query": params})
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/v1/load", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doRequest(req, auth)
}

type dimensionSearchParams struct {
	Dimension string `json:"dimension" jsonschema:"The dimension to search (e.g. \"Stores.name\")."`
	Query     string `json:"query" jsonschema:"Search term to match against dimension values (case-insensitive contains)."`
}

func (c *cubeClient) dimensionSearch(ctx context.Context, auth string, params dimensionSearchParams) (*mcp.CallToolResult, error) {
	if params.Dimension == "" || params.Query == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "dimension and query are required"}},
		}, nil
	}

	cubeQuery := map[string]any{
		"dimensions": []string{params.Dimension},
		"filters": []map[string]any{
			{
				"member":   params.Dimension,
				"operator": "contains",
				"values":   []string{params.Query},
			},
		},
		"limit": 100,
	}

	body, err := json.Marshal(map[string]any{"query": cubeQuery})
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/v1/load", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doRequest(req, auth)
}

func extractAuth(req *mcp.CallToolRequest) string {
	if req == nil || req.Extra == nil || req.Extra.Header == nil {
		return ""
	}
	return req.Extra.Header.Get("Authorization")
}

func requireEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("missing required environment variable: %s", name)
	}
	return v
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8003"
	}

	cube := newCubeClient(requireEnv("CUBE_API_URL"))

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "cube-mcp",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name: "meta",
		Description: "Returns the Cube semantic model: cubes, views, measures, dimensions, joins, " +
			"and descriptions. Call this first to understand what data is available before " +
			"formulating queries.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ metaParams) (*mcp.CallToolResult, any, error) {
		res, err := cube.meta(ctx, extractAuth(req))
		return res, nil, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "query",
		Description: "Executes a structured Cube query and returns results. " +
			"Use meta first to discover available measures and dimensions.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in queryParams) (*mcp.CallToolResult, any, error) {
		res, err := cube.query(ctx, extractAuth(req), in)
		return res, nil, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "dimension_search",
		Description: "Searches for matching values of a dimension. Use this to resolve ambiguous " +
			"references like store names, product names, or categories before querying.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in dimensionSearchParams) (*mcp.CallToolResult, any, error) {
		res, err := cube.dimensionSearch(ctx, extractAuth(req), in)
		return res, nil, err
	})

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	commitSHA := os.Getenv("COMMIT_SHA")
	if commitSHA == "" {
		commitSHA = "unknown"
	}

	addr := ":" + port
	log.Printf("cube-mcp listening on %s (commit=%s)", addr, commitSHA)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
