package rp

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// capturedReq is what the stub server saw.
type capturedReq struct {
	method string
	path   string
	body   map[string]any
}

// withStub swaps the package URLs to a local stub, runs fn with a client
// pointed at it, and restores the globals afterwards. The assertions live in fn.
func withStub(t *testing.T, fn func(req *capturedReq, c *Client)) {
	t.Helper()
	prevBase, prevApp := BaseURL, AppBaseURL
	var got capturedReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if ce := r.Header.Get("Content-Encoding"); ce == "gzip" {
			zr, err := gzip.NewReader(r.Body)
			if err != nil {
				t.Errorf("gzip reader: %v", err)
				http.Error(w, "bad gzip", 400)
				return
			}
			body, _ = io.ReadAll(zr)
		} else {
			body, _ = io.ReadAll(r.Body)
		}
		got = capturedReq{method: r.Method, path: r.URL.Path}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &got.body); err != nil {
				t.Errorf("body not JSON: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()
	BaseURL, AppBaseURL = srv.URL, srv.URL
	defer func() { BaseURL, AppBaseURL = prevBase, prevApp }()
	fn(&got, New("tok", 42, srv.Client()))
}

func TestReplaceDayExerciseSendsRepeat(t *testing.T) {
	withStub(t, func(req *capturedReq, c *Client) {
		if _, err := c.ReplaceDayExercise(context.Background(), 100, 200, 999, true); err != nil {
			t.Fatal(err)
		}
		if req.method != http.MethodPut {
			t.Errorf("method = %s, want PUT", req.method)
		}
		if req.path != "/training/42/days/100/exercises/200" {
			t.Errorf("path = %s", req.path)
		}
		if req.body["newExerciseId"] != float64(999) || req.body["repeat"] != true {
			t.Errorf("body = %v", req.body)
		}
	})
}

func TestReorderDayExercises(t *testing.T) {
	withStub(t, func(req *capturedReq, c *Client) {
		if _, err := c.ReorderDayExercises(context.Background(), 100, []int{3, 2, 1}, true); err != nil {
			t.Fatal(err)
		}
		if req.method != http.MethodPut {
			t.Errorf("method = %s, want PUT", req.method)
		}
		if req.path != "/training/42/days/100/exercises/order" {
			t.Errorf("path = %s", req.path)
		}
		order, ok := req.body["order"].([]any)
		if !ok || len(order) != 3 || order[0] != float64(3) || order[2] != float64(1) {
			t.Errorf("order = %v", req.body["order"])
		}
		if req.body["repeat"] != true {
			t.Errorf("repeat = %v", req.body["repeat"])
		}
	})
}

func TestAddSetSendsPosition(t *testing.T) {
	withStub(t, func(req *capturedReq, c *Client) {
		if _, err := c.AddSet(context.Background(), 100, 200, 1); err != nil {
			t.Fatal(err)
		}
		if req.method != http.MethodPost {
			t.Errorf("method = %s, want POST", req.method)
		}
		if want := "/training/42/days/100/exercises/200/sets"; req.path != want {
			t.Errorf("path = %s, want %s", req.path, want)
		}
		if req.body["position"] != float64(1) {
			t.Errorf("position = %v", req.body["position"])
		}
	})
}

func TestAddExerciseToDayOmitsUnsetFields(t *testing.T) {
	withStub(t, func(req *capturedReq, c *Client) {
		if _, err := c.AddExerciseToDay(context.Background(), 100, 847028, nil, nil, true); err != nil {
			t.Fatal(err)
		}
		if req.method != http.MethodPost {
			t.Errorf("method = %s, want POST", req.method)
		}
		if req.path != "/training/42/days/100/exercises" {
			t.Errorf("path = %s", req.path)
		}
		for _, k := range []string{"muscleGroupId", "position"} {
			if _, present := req.body[k]; present {
				t.Errorf("%s should be omitted, body = %v", k, req.body)
			}
		}
		if req.body["exerciseId"] != float64(847028) || req.body["repeat"] != true {
			t.Errorf("body = %v", req.body)
		}
	})
}

func TestCreateAndUpdateExercisePaths(t *testing.T) {
	withStub(t, func(req *capturedReq, c *Client) {
		if _, err := c.CreateExercise(context.Background(), json.RawMessage(`{"name":"Band-Assisted Dip","muscleGroupId":3}`)); err != nil {
			t.Fatal(err)
		}
		if req.method != http.MethodPost || req.path != "/training/42/exercises" {
			t.Errorf("%s %s", req.method, req.path)
		}
	})
	withStub(t, func(req *capturedReq, c *Client) {
		if _, err := c.UpdateExercise(context.Background(), 847463, json.RawMessage(`{"exerciseType":"machine-assistance"}`)); err != nil {
			t.Fatal(err)
		}
		if req.method != http.MethodPut {
			t.Errorf("method = %s, want PUT", req.method)
		}
		if req.path != "/training/42/exercises/847463" {
			t.Errorf("path = %s", req.path)
		}
		if req.body["exerciseType"] != "machine-assistance" {
			t.Errorf("body = %v", req.body)
		}
	})
}

func TestDeleteDayExercisesUsesMesocycleRoute(t *testing.T) {
	withStub(t, func(req *capturedReq, c *Client) {
		if err := c.DeleteDayExercises(context.Background(), "abc123", []int{11, 12}); err != nil {
			t.Fatal(err)
		}
		if req.method != http.MethodPost {
			t.Errorf("method = %s, want POST", req.method)
		}
		want := "/training/42/mesocycles/abc123/day-exercises/delete"
		if req.path != want {
			t.Errorf("path = %s, want %s", req.path, want)
		}
		ids, ok := req.body["dayExerciseIds"].([]any)
		if !ok || len(ids) != 2 || ids[0] != float64(11) {
			t.Errorf("dayExerciseIds = %v", req.body["dayExerciseIds"])
		}
	})
}

func TestDeleteDayExerciseRefusesWithoutMesoKey(t *testing.T) {
	c := New("tok", 42, nil)
	err := c.DeleteDayExercise(context.Background(), 1, 2)
	if err == nil || !strings.Contains(err.Error(), "deleteDayExercises") {
		t.Errorf("expected guidance error, got %v", err)
	}
}
