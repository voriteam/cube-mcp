package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testAuth = "Bearer test-token"

func TestMetaForwardsAuth(t *testing.T) {
	expected := map[string]any{"cubes": []any{}}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/cubejs-api/v1/meta" {
			t.Errorf("expected /cubejs-api/v1/meta, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != testAuth {
			t.Errorf("expected Authorization %q, got %q", testAuth, got)
		}
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer ts.Close()

	c := newCubeClient(ts.URL + "/cubejs-api")
	res, err := c.meta(context.Background(), testAuth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}
}

func TestMetaMissingAuthPassesThrough(t *testing.T) {
	// cube-mcp does not mint tokens — a missing caller auth header is
	// forwarded as-is (i.e. not set), letting Cube return its own 401.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("expected no Authorization header, got %q", got)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer ts.Close()

	c := newCubeClient(ts.URL + "/cubejs-api")
	res, err := c.meta(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError for upstream 401")
	}
}

func TestQueryForwardsAuthAndBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/cubejs-api/v1/load" {
			t.Errorf("expected /cubejs-api/v1/load, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != testAuth {
			t.Errorf("expected Authorization %q, got %q", testAuth, got)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		body, _ := io.ReadAll(r.Body)
		var envelope map[string]any
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatalf("invalid request body: %v", err)
		}
		query, ok := envelope["query"].(map[string]any)
		if !ok {
			t.Fatalf("expected query envelope, got %v", envelope)
		}
		measures, ok := query["measures"].([]any)
		if !ok || len(measures) != 1 || measures[0] != "Orders.count" {
			t.Errorf("unexpected measures: %v", query["measures"])
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"Orders.count": "42"}},
		})
	}))
	defer ts.Close()

	c := newCubeClient(ts.URL + "/cubejs-api")
	res, err := c.query(context.Background(), testAuth, queryParams{
		Measures:   []string{"Orders.count"},
		Dimensions: []string{"Orders.status"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}
}

func TestDimensionSearch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != testAuth {
			t.Errorf("expected Authorization %q, got %q", testAuth, got)
		}
		body, _ := io.ReadAll(r.Body)
		var envelope map[string]any
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatalf("invalid request body: %v", err)
		}
		query, ok := envelope["query"].(map[string]any)
		if !ok {
			t.Fatalf("expected query envelope, got %v", envelope)
		}
		dimensions, ok := query["dimensions"].([]any)
		if !ok || len(dimensions) != 1 || dimensions[0] != "Stores.name" {
			t.Errorf("unexpected dimensions: %v", query["dimensions"])
		}
		filters, ok := query["filters"].([]any)
		if !ok || len(filters) != 1 {
			t.Fatalf("expected 1 filter, got %v", query["filters"])
		}
		filter := filters[0].(map[string]any)
		if filter["operator"] != "contains" {
			t.Errorf("expected contains operator, got %v", filter["operator"])
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"Stores.name": "Caribbean Supercenter"}},
		})
	}))
	defer ts.Close()

	c := newCubeClient(ts.URL + "/cubejs-api")
	res, err := c.dimensionSearch(context.Background(), testAuth, dimensionSearchParams{
		Dimension: "Stores.name",
		Query:     "Caribbean",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}
}

func TestDimensionSearchValidation(t *testing.T) {
	c := newCubeClient("http://localhost:4000/cubejs-api")

	res, err := c.dimensionSearch(context.Background(), testAuth, dimensionSearchParams{Dimension: "Stores.name"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError for missing query")
	}

	res, err = c.dimensionSearch(context.Background(), testAuth, dimensionSearchParams{Query: "Caribbean"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError for missing dimension")
	}
}

func TestHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer ts.Close()

	c := newCubeClient(ts.URL + "/cubejs-api")
	res, err := c.meta(context.Background(), testAuth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError for HTTP 500")
	}
}

func TestTrailingSlashNormalization(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cubejs-api/v1/meta" {
			t.Errorf("expected /cubejs-api/v1/meta, got %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"cubes": []any{}})
	}))
	defer ts.Close()

	c := newCubeClient(ts.URL + "/cubejs-api/")
	res, err := c.meta(context.Background(), testAuth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}
}

func TestExtractAuth(t *testing.T) {
	// Nil request / extra / header all return empty string.
	if got := extractAuth(nil); got != "" {
		t.Errorf("nil request: expected empty, got %q", got)
	}
}
