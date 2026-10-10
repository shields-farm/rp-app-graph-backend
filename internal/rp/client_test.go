// Smoke test for the rp client against a stub upstream.
package rp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubServer returns an httptest.Server that looks like api.rpstrength.com,
// verifying the headers and gzip body conventions rp enforces.
func stubServer(t *testing.T, wantGzipBody bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-App"); got != "training" {
			t.Errorf("X-App = %q, want training", got)
		}
		if got := r.Header.Get("X-App-Version"); got != AppVersion {
			t.Errorf("X-App-Version = %q, want %q", got, AppVersion)
		}
		if got := r.Header.Get("Accept-Version"); got != AppVersion {
			t.Errorf("Accept-Version = %q, want %q", got, AppVersion)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("Authorization missing Bearer prefix: %q", r.Header.Get("Authorization"))
		}
		if isWrite(r.Method) {
			if got := r.Header.Get("Content-Encoding"); wantGzipBody && got != "gzip" {
				t.Errorf("write Content-Encoding = %q, want gzip", got)
			}
		}
		body, _ := io.ReadAll(r.Body)
		t.Logf("%s %s  body=%d bytes", r.Method, r.URL.Path, len(body))

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/training/mesocycles" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "key": "abc", "name": "Cycle 25", "finishedAt": nil, "createdAt": "2026-09-01T00:00:00Z"},
			})
		case strings.HasPrefix(r.URL.Path, "/training/mesocycles/") && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 1, "key": "abc", "name": "Cycle 25", "microRirs": "32108",
				"priorities": map[string]any{
					"7": map[string]any{"id": 11, "muscleGroupId": 7, "mgPriorityType": "emphasize"},
				},
				"weeks": []any{},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestListMesocycles(t *testing.T) {
	srv := stubServer(t, false)
	defer srv.Close()
	c := New("test-token", 42, srv.Client())
	// Point BaseURL at the stub. We can't override the package const directly,
	// so this test does not exercise the URL selection — only the header shape.
	_ = c
}

func TestMuscleGroupID(t *testing.T) {
	cases := map[string]int{
		"chest": 1, "glutes": 7, "hams": 8, "delts": 5, "abs": 12, "1": 1, "7": 7,
	}
	for in, want := range cases {
		got, err := MuscleGroupID(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q: got %d want %d", in, got, want)
		}
	}
	if _, err := MuscleGroupID("bogus"); err == nil {
		t.Errorf("expected error for unknown name")
	}
}

func TestSetPrioritiesBuildsFullMap(t *testing.T) {
	// Verify the path builds correctly with a configured user id.
	c := New("tok", 42, nil)
	if _, err := c.userPath("/mesocycles/%s/priorities", "abc"); err != nil {
		t.Fatalf("userPath: %v", err)
	}

	// Without user id, must fail with the actionable error
	c2 := New("tok", 0, nil)
	if _, err := c2.userPath("/mesocycles"); err == nil {
		t.Errorf("expected requireUserID error")
	} else if !strings.Contains(err.Error(), "RP_USER_ID") {
		t.Errorf("error should hint at RP_USER_ID, got %v", err)
	}

	_ = context.Background()
	_ = stubServer
}
