// Package rp is the native Go client for the RP Strength (Hypertrophy App)
// REST API. It is a line-for-line port of ~/.hermes/scripts/rp-lib/rp_api.py
// semantics: same base URLs, same headers, same gzip-on-write, same quirks
// that were learned the hard way (partial-priorities 500s, app-proxy writes,
// Accept-Version pinning).
//
// Auth is NOT this package's concern. Callers hand us an RP bearer token per
// request via WithToken — no storage, no refresh, no Safari.
package rp

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	BaseURL   = "https://api.rpstrength.com"
	AppBaseURL = "https://training.rpstrength.com/api"
	// AppVersion is pinned the way rp_api.py pins it; RP accepts anything
	// recent, but pinning avoids surprises if they gate.
	AppVersion = "1.3.8"
	// GqlgenMaxBody guards against pathological mutation payloads.
	maxBody = 1 << 20
)

// Client is a thin wrapper over net/http with the RP conventions baked in.
// Do not share one across requests with different tokens.
type Client struct {
	token  string
	userID int // RP numeric user id; required for write paths, optional for reads
	hc     *http.Client
}

// New returns a Client using an injected http.Client. userID may be 0 for
// read-only clients — write paths will refuse with a clear error if so.
func New(token string, userID int, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{token: token, userID: userID, hc: hc}
}

// requireUserID returns an actionable error when a write path is invoked
// without a configured user id.
func (c *Client) requireUserID() (int, error) {
	if c.userID <= 0 {
		return 0, errors.New("RP user id is not configured (set RP_USER_ID on the server, or pass X-RP-User-Id per request); write operations require it")
	}
	return c.userID, nil
}

// userPath resolves the user-scoped path and returns an error if no user id is configured.
func (c *Client) userPath(format string, args ...any) (string, error) {
	uid, err := c.requireUserID()
	if err != nil {
		return "", err
	}
	full := fmt.Sprintf("/training/%d"+format, append([]any{uid}, args...)...)
	return full, nil
}

// APIError is the non-2xx response body verbatim, useful for debugging
// against the real RP service.
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("RP API %s %s → %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// ErrUnauthorized lets callers distinguish an expired RP token from other
// failures so the graph layer can hint at the fix.
var ErrUnauthorized = errors.New("unauthorized")

func isWrite(method string) bool {
	switch method {
	case http.MethodPut, http.MethodPost, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func (c *Client) do(ctx context.Context, method, path string, body any, result any) error {
	var url string
	if isWrite(method) {
		url = fmt.Sprintf("%s%s?v=%s", AppBaseURL, path, AppVersion)
	} else {
		url = fmt.Sprintf("%s%s", BaseURL, path)
	}

	var bodyReader io.Reader
	headers := map[string]string{
		"Authorization": "Bearer " + c.token,
		"Accept":        "application/json",
		"Accept-Version": AppVersion,
		"X-App":         "training",
		"X-App-Version": AppVersion,
	}
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		if len(raw) > maxBody {
			return fmt.Errorf("body too large (%d bytes)", len(raw))
		}
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		if _, err := gw.Write(raw); err != nil {
			return fmt.Errorf("gzip: %w", err)
		}
		if err := gw.Close(); err != nil {
			return fmt.Errorf("gzip close: %w", err)
		}
		bodyReader = &buf
		headers["Content-Type"] = "application/json"
		headers["Content-Encoding"] = "gzip"
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: RP bearer token rejected (%d)", ErrUnauthorized, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		text, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &APIError{
			StatusCode: resp.StatusCode,
			Method:     method,
			Path:       path,
			Body:       strings.TrimSpace(string(text)),
		}
	}
	if result == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(result)
}

// ─── Mesocycles ─────────────────────────────────────────────────────────────

func (c *Client) ListMesocycles(ctx context.Context) ([]MesocycleSummary, error) {
	var out []MesocycleSummary
	if err := c.do(ctx, http.MethodGet, "/training/mesocycles", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetMesocycle(ctx context.Context, key string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/training/mesocycles/"+key, nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// FindMesocycle mirrors rp_api.find_mesocycle: exact name match, "cycle N"
// prefix, then substring.
func (c *Client) FindMesocycle(ctx context.Context, keyOrNameOrNumber string) (json.RawMessage, error) {
	mesos, err := c.ListMesocycles(ctx)
	if err != nil {
		return nil, err
	}
	s := strings.ToLower(strings.TrimSpace(keyOrNameOrNumber))

	for _, m := range mesos {
		if strings.ToLower(m.Name) == s {
			return c.GetMesocycle(ctx, m.Key)
		}
	}
	if n, ok := parseCycleNumber(s); ok {
		prefix := fmt.Sprintf("cycle %d", n)
		for _, m := range mesos {
			if strings.HasPrefix(strings.ToLower(m.Name), prefix) {
				return c.GetMesocycle(ctx, m.Key)
			}
		}
	}
	for _, m := range mesos {
		if strings.Contains(strings.ToLower(m.Name), s) {
			return c.GetMesocycle(ctx, m.Key)
		}
	}
	return nil, fmt.Errorf("no mesocycle matching %q", keyOrNameOrNumber)
}

func parseCycleNumber(s string) (int, bool) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err == nil && n > 0 {
		return n, true
	}
	return 0, false
}

// ActiveMesocycle returns the cycle whose finishedAt is null.
func (c *Client) ActiveMesocycle(ctx context.Context) (json.RawMessage, error) {
	mesos, err := c.ListMesocycles(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range mesos {
		if m.FinishedAt == nil {
			return c.GetMesocycle(ctx, m.Key)
		}
	}
	return nil, fmt.Errorf("no active mesocycle")
}

// ─── Writes ────────────────────────────────────────────────────────────────

func (c *Client) CreateMesocycle(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	var raw json.RawMessage
	path, err := c.userPath("/mesocycles")
	if err != nil {
		return nil, err
	}
	err = c.do(ctx, http.MethodPost, path, payload, &raw)
	return raw, err
}

func (c *Client) UpdateMesocycle(ctx context.Context, key string, payload json.RawMessage) (json.RawMessage, error) {
	var raw json.RawMessage
	path, err := c.userPath("/mesocycles/%s", key)
	if err != nil {
		return nil, err
	}
	err = c.do(ctx, http.MethodPut, path, payload, &raw)
	return raw, err
}

func (c *Client) DeleteMesocycle(ctx context.Context, key string) error {
	path, err := c.userPath("/mesocycles/%s", key)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) AddMicro(ctx context.Context, key string) (json.RawMessage, error) {
	var raw json.RawMessage
	path, err := c.userPath("/mesocycles/%s/add-micro", key)
	if err != nil {
		return nil, err
	}
	err = c.do(ctx, http.MethodPost, path, struct{}{}, &raw)
	return raw, err
}

func (c *Client) RemoveMicro(ctx context.Context, key string) error {
	path, err := c.userPath("/mesocycles/%s/remove-micro", key)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// SetPriorities mirrors rp_api.set_priorities: the endpoint does delete-then-
// insert with no transaction, so it requires the FULL map of existing rows
// with new values merged in.
func (c *Client) SetPriorities(ctx context.Context, mesoKey string, changes map[string]string) (json.RawMessage, error) {
	current, err := c.GetMesocycle(ctx, mesoKey)
	if err != nil {
		return nil, err
	}
	var detail struct {
		Priorities map[string]struct {
			ID             any    `json:"id"`
			MuscleGroupID  int    `json:"muscleGroupId"`
			MgPriorityType string `json:"mgPriorityType"`
		} `json:"priorities"`
	}
	if err := json.Unmarshal(current, &detail); err != nil {
		return nil, fmt.Errorf("parse priorities: %w", err)
	}

	// Build the full payload with existing IDs preserved.
	payload := map[string]map[string]any{}
	for k, v := range detail.Priorities {
		payload[k] = map[string]any{
			"id":             v.ID,
			"muscleGroupId":  v.MuscleGroupID,
			"mgPriorityType": v.MgPriorityType,
		}
	}
	for mg, tier := range changes {
		mid, err := MuscleGroupID(mg)
		if err != nil {
			return nil, err
		}
		k := fmt.Sprint(mid)
		if payload[k] == nil {
			payload[k] = map[string]any{"muscleGroupId": mid}
		}
		payload[k]["mgPriorityType"] = tier
	}
	path, err := c.userPath("/mesocycles/%s/priorities", mesoKey)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	err = c.do(ctx, http.MethodPut, path, payload, &raw)
	return raw, err
}

// ─── Days ───────────────────────────────────────────────────────────────────

func (c *Client) UpdateDay(ctx context.Context, dayID int, payload json.RawMessage) (json.RawMessage, error) {
	var raw json.RawMessage
	path, err := c.userPath("/days/%d", dayID)
	if err != nil {
		return nil, err
	}
	err = c.do(ctx, http.MethodPut, path, payload, &raw)
	return raw, err
}

func (c *Client) UpdateDayBodyweight(ctx context.Context, dayID int, bodyweight float64) (json.RawMessage, error) {
	var raw json.RawMessage
	path, err := c.userPath("/days/%d/bodyweight", dayID)
	if err != nil {
		return nil, err
	}
	err = c.do(ctx, http.MethodPut, path, map[string]any{"bodyweight": bodyweight}, &raw)
	return raw, err
}

func (c *Client) UpdateDayLabel(ctx context.Context, dayID int, label string) (json.RawMessage, error) {
	var raw json.RawMessage
	path, err := c.userPath("/days/%d/label", dayID)
	if err != nil {
		return nil, err
	}
	err = c.do(ctx, http.MethodPut, path, map[string]any{"label": label}, &raw)
	return raw, err
}

func (c *Client) AddDayNote(ctx context.Context, dayID int, text string) (json.RawMessage, error) {
	var raw json.RawMessage
	path, err := c.userPath("/days/%d/notes", dayID)
	if err != nil {
		return nil, err
	}
	err = c.do(ctx, http.MethodPost, path, map[string]any{"text": text}, &raw)
	return raw, err
}

// ─── Exercises ──────────────────────────────────────────────────────────────

func (c *Client) ListExercises(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.do(ctx, http.MethodGet, "/training/exercises", nil, &raw)
	return raw, err
}

func (c *Client) ExerciseHistory(ctx context.Context, exerciseID int) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/training/exercises/%d/history", exerciseID), nil, &raw)
	return raw, err
}

func (c *Client) AddExerciseToDay(ctx context.Context, dayID, exerciseID, muscleGroupID, position int) (json.RawMessage, error) {
	var raw json.RawMessage
	path, err := c.userPath("/days/%d/exercises", dayID)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"exerciseId": exerciseID, "muscleGroupId": muscleGroupID, "position": position}
	err = c.do(ctx, http.MethodPost, path, body, &raw)
	return raw, err
}

func (c *Client) UpdateDayExercise(ctx context.Context, dayID, dayExerciseID int, payload json.RawMessage) (json.RawMessage, error) {
	var raw json.RawMessage
	path, err := c.userPath("/days/%d/exercises/%d", dayID, dayExerciseID)
	if err != nil {
		return nil, err
	}
	err = c.do(ctx, http.MethodPut, path, payload, &raw)
	return raw, err
}

func (c *Client) DeleteDayExercise(ctx context.Context, dayID, dayExerciseID int) error {
	path, err := c.userPath("/days/%d/exercises/%d", dayID, dayExerciseID)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// ─── Sets ───────────────────────────────────────────────────────────────────

func (c *Client) UpdateSet(ctx context.Context, setID int, payload json.RawMessage) (json.RawMessage, error) {
	var raw json.RawMessage
	path, err := c.userPath("/sets/%d", setID)
	if err != nil {
		return nil, err
	}
	err = c.do(ctx, http.MethodPut, path, payload, &raw)
	return raw, err
}

func (c *Client) DeleteSet(ctx context.Context, setID int) error {
	path, err := c.userPath("/sets/%d", setID)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// ─── Templates ──────────────────────────────────────────────────────────────

func (c *Client) ListTemplates(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	path, err := c.userPath("/templates")
	if err != nil {
		return nil, err
	}
	err = c.do(ctx, http.MethodGet, path, nil, &raw)
	return raw, err
}

func (c *Client) GetTemplate(ctx context.Context, key string) (json.RawMessage, error) {
	var raw json.RawMessage
	path, err := c.userPath("/templates/%s", key)
	if err != nil {
		return nil, err
	}
	err = c.do(ctx, http.MethodGet, path, nil, &raw)
	return raw, err
}

func (c *Client) CreateTemplate(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	var raw json.RawMessage
	path, err := c.userPath("/templates")
	if err != nil {
		return nil, err
	}
	err = c.do(ctx, http.MethodPost, path, payload, &raw)
	return raw, err
}

func (c *Client) UpdateTemplate(ctx context.Context, key string, payload json.RawMessage) (json.RawMessage, error) {
	var raw json.RawMessage
	path, err := c.userPath("/templates/%s", key)
	if err != nil {
		return nil, err
	}
	err = c.do(ctx, http.MethodPut, path, payload, &raw)
	return raw, err
}

func (c *Client) DeleteTemplate(ctx context.Context, key string) error {
	path, err := c.userPath("/templates/%s", key)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// ─── User ───────────────────────────────────────────────────────────────────

func (c *Client) UserAttributes(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.do(ctx, http.MethodGet, "/training/user-attributes", nil, &raw)
	return raw, err
}

func (c *Client) UserExerciseHistory(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.do(ctx, http.MethodGet, "/training/user-exercise-history", nil, &raw)
	return raw, err
}

func (c *Client) Bootstrap(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.do(ctx, http.MethodGet, "/training/bootstrap", nil, &raw)
	return raw, err
}

// ─── Muscle-group helpers (used by both client and resolvers) ───────────────

var MuscleGroupNames = map[int]string{
	1: "Chest", 2: "Back", 3: "Triceps", 4: "Biceps",
	5: "Side Delts", 6: "Quads", 7: "Glutes/Spinal Erectors",
	8: "Hamstrings", 9: "Calves", 10: "Traps", 11: "Forearms", 12: "Abs",
}

var muscleGroupAliases = map[string]int{
	"chest": 1, "back": 2, "triceps": 3, "biceps": 4,
	"delts": 5, "side delts": 5, "shoulders": 5,
	"quad": 6, "quads": 6,
	"glute": 7, "glutes": 7, "glutes/spinal erectors": 7,
	"ham": 8, "hams": 8, "hamstring": 8, "hamstrings": 8,
	"calf": 9, "calves": 9,
	"traps": 10, "forearms": 11,
	"abs": 12, "core": 12,
}

// MuscleGroupID accepts an int-as-string or a name/alias (same rules as
// rp_api.mg_id).
func MuscleGroupID(nameOrID string) (int, error) {
	s := strings.ToLower(strings.TrimSpace(nameOrID))
	if n, err := parseInt(s); err == nil {
		return n, nil
	}
	if n, ok := muscleGroupAliases[s]; ok {
		return n, nil
	}
	return 0, fmt.Errorf("unknown muscle group %q (known: %s)", nameOrID, knownNames())
}

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

func knownNames() string {
	names := make([]string, 0, len(muscleGroupAliases))
	for k := range muscleGroupAliases {
		names = append(names, k)
	}
	return strings.Join(names, ", ")
}

// ─── List shape for ListMesocycles ──────────────────────────────────────────

// MesocycleSummary is the row shape returned by /training/mesocycles.
type MesocycleSummary struct {
	ID         int        `json:"id"`
	Key        string     `json:"key"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	CreatedAt  *time.Time `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt"`
	WeekCount  *int       `json:"weekCount"`
}
