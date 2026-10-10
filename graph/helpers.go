package graph

// This file is hand-written. gqlgen owns schema.resolvers.go (which we delete
// on regen). These helpers implement the heavy lifting for the resolvers.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shields-farm/rp-app-graph-backend/graph/model"
	"github.com/shields-farm/rp-app-graph-backend/internal/rp"
	"github.com/shields-farm/rp-app-graph-backend/internal/server"
)

// fetchExerciseLibrary grabs the exercise library once for the request, so a
// nested mesocycle query can resolve exerciseId → name without N+1 calls.
func fetchExerciseLibrary(ctx context.Context, c *rp.Client) (map[int]*exerciseDTO, error) {
	raw, err := c.ListExercises(ctx)
	if err != nil {
		return nil, err
	}
	var list []exerciseDTO
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("parse exercises: %w", err)
	}
	out := make(map[int]*exerciseDTO, len(list))
	for i := range list {
		out[list[i].ID] = &list[i]
	}
	return out, nil
}

// decodeMesocycleResolved is decodeMesocycleWithLib + a single upstream call
// for the exercise library, then resolves names onto every day exercise.
func decodeMesocycleResolved(ctx context.Context, c *rp.Client, raw json.RawMessage) (*model.Mesocycle, error) {
	lib, err := fetchExerciseLibrary(ctx, c)
	if err != nil {
		return nil, err
	}
	return decodeMesocycleWithLib(raw, lib)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func (r *Resolver) client(ctx context.Context) (*rp.Client, error) {
	return server.NewRPClient(ctx, r.DefaultUserID())
}

// decode is a tolerant unmarshal: it ignores RP fields we don't model.
func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty response")
	}
	return json.Unmarshal(raw, v)
}

// raw returns the original payload, useful when callers need a field we didn't model.
func rawMsg(raw json.RawMessage) json.RawMessage { return raw }

// toPriorities flattens the API's priority map into the schema's list.
func toPriorities(m map[string]struct {
	ID             any    `json:"id"`
	MuscleGroupID  int    `json:"muscleGroupId"`
	MgPriorityType string `json:"mgPriorityType"`
},
) []*model.MuscleGroupPriority {
	out := make([]*model.MuscleGroupPriority, 0, len(m))
	for _, v := range m {
		name := rp.MuscleGroupNames[v.MuscleGroupID]
		if name == "" {
			name = fmt.Sprintf("MG %d", v.MuscleGroupID)
		}
		var id *string
		if v.ID != nil {
			s := fmt.Sprint(v.ID)
			id = &s
		}
		out = append(out, &model.MuscleGroupPriority{
			ID:              id,
			MuscleGroupID:   v.MuscleGroupID,
			MuscleGroupName: name,
			Tier:            model.PriorityTier(v.MgPriorityType),
		})
	}
	return out
}

func weekFromRaw(idx int, raw json.RawMessage, exLib map[int]*exerciseDTO) (*model.Week, error) {
	// RP weeks have only {days: [...]}. id/position are positional.
	var dto struct {
		Days []json.RawMessage `json:"days"`
	}
	if err := decode(raw, &dto); err != nil {
		return nil, err
	}
	out := &model.Week{ID: idx, Position: idx, Raw: raw}
	for _, dRaw := range dto.Days {
		d, err := decodeDay(dRaw)
		if err != nil {
			return nil, err
		}
		out.Days = append(out.Days, d)
	}
	return out, nil
}

// decodeMesocycle maps the meso detail payload into the gqlgen shape.
// Field names follow the RP API; unknown extras are kept in Raw.
// exLib, when non-nil, is used to resolve exerciseId → Exercise on day exercises
// (the meso payload doesn't embed exercise detail).
func decodeMesocycleWithLib(raw json.RawMessage, exLib map[int]*exerciseDTO) (*model.Mesocycle, error) {
	var dto struct {
		ID         int             `json:"id"`
		Key        string          `json:"key"`
		Name       string          `json:"name"`
		Status     *string         `json:"status"`
		CreatedAt  *string         `json:"createdAt"`
		FinishedAt *string         `json:"finishedAt"`
		MicroRirs  json.RawMessage `json:"microRirs"`
		Priorities map[string]struct {
			ID             any    `json:"id"`
			MuscleGroupID  int    `json:"muscleGroupId"`
			MgPriorityType string `json:"mgPriorityType"`
		} `json:"priorities"`
		Weeks []json.RawMessage `json:"weeks"`
	}
	if err := decode(raw, &dto); err != nil {
		return nil, err
	}
	out := &model.Mesocycle{
		ID:         dto.ID,
		Key:        dto.Key,
		Name:       dto.Name,
		Status:     dto.Status,
		MicroRirs:  rawToString(dto.MicroRirs),
		Priorities: toPriorities(dto.Priorities),
		Raw:        raw,
	}
	out.CreatedAt = parseTime(dto.CreatedAt)
	out.FinishedAt = parseTime(dto.FinishedAt)
	for i, wRaw := range dto.Weeks {
		w, err := weekFromRaw(i, wRaw, exLib)
		if err != nil {
			return nil, err
		}
		out.Weeks = append(out.Weeks, w)
	}
	// Resolve exercise names from the library when callers asked for the meso detail.
	if exLib != nil {
		for _, w := range out.Weeks {
			for _, d := range w.Days {
				for _, de := range d.Exercises {
					if de.Exercise == nil && de.ExerciseID != nil && *de.ExerciseID > 0 {
						if ex, ok := exLib[*de.ExerciseID]; ok {
							de.Exercise = ex.toModel()
						}
					}
				}
			}
		}
	}
	return out, nil
}

func decodeMesocycle(raw json.RawMessage) (*model.Mesocycle, error) {
	return decodeMesocycleWithLib(raw, nil)
}

func decodeWeek(raw json.RawMessage) (*model.Week, error) {
	var dto struct {
		ID       int               `json:"id"`
		Position int               `json:"position"`
		Days     []json.RawMessage `json:"days"`
	}
	if err := decode(raw, &dto); err != nil {
		return nil, err
	}
	out := &model.Week{ID: dto.ID, Position: dto.Position, Raw: raw}
	for _, dRaw := range dto.Days {
		d, err := decodeDay(dRaw)
		if err != nil {
			return nil, err
		}
		out.Days = append(out.Days, d)
	}
	return out, nil
}

func decodeDay(raw json.RawMessage) (*model.Day, error) {
	var dto struct {
		ID         int               `json:"id"`
		Label      *string           `json:"label"`
		Bodyweight *float64          `json:"bodyweight"`
		FinishedAt *string           `json:"finishedAt"`
		Exercises  []json.RawMessage `json:"exercises"`
	}
	if err := decode(raw, &dto); err != nil {
		return nil, err
	}
	out := &model.Day{
		ID:         dto.ID,
		Label:      dto.Label,
		Bodyweight: dto.Bodyweight,
		FinishedAt: parseTime(dto.FinishedAt),
		Raw:        raw,
	}
	for _, eRaw := range dto.Exercises {
		e, err := decodeDayExercise(eRaw)
		if err != nil {
			return nil, err
		}
		out.Exercises = append(out.Exercises, e)
	}
	return out, nil
}

func decodeDayExercise(raw json.RawMessage) (*model.DayExercise, error) {
	var dto struct {
		ID           int               `json:"id"`
		Position     int               `json:"position"`
		Exercise     *exerciseDTO      `json:"exercise"`
		ExerciseID   *int              `json:"exerciseId"`
		MuscleGroupID *int              `json:"muscleGroupId"`
		MuscleGroupName *string         `json:"muscleGroupName"`
		Sets         []json.RawMessage `json:"sets"`
	}
	if err := decode(raw, &dto); err != nil {
		return nil, err
	}
	out := &model.DayExercise{
		ID:              dto.ID,
		Position:        dto.Position,
		ExerciseID:      dto.ExerciseID,
		MuscleGroupID:   dto.MuscleGroupID,
		MuscleGroupName: dto.MuscleGroupName,
		Raw:             raw,
	}
	if dto.Exercise != nil {
		out.Exercise = dto.Exercise.toModel()
	}
	for _, sRaw := range dto.Sets {
		s, err := decodeSet(sRaw)
		if err != nil {
			return nil, err
		}
		out.Sets = append(out.Sets, s)
	}
	return out, nil
}

type exerciseDTO struct {
	ID              int     `json:"id"`
	Name            string  `json:"name"`
	MuscleGroupID   *int    `json:"muscleGroupId"`
	MuscleGroupName *string `json:"muscleGroupName"`
	Raw             json.RawMessage
}

func (e *exerciseDTO) UnmarshalJSON(b []byte) error {
	type alias exerciseDTO
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*e = exerciseDTO(a)
	e.Raw = append([]byte(nil), b...)
	return nil
}

func (e *exerciseDTO) toModel() *model.Exercise {
	if e == nil {
		return nil
	}
	return &model.Exercise{
		ID:              e.ID,
		Name:            e.Name,
		MuscleGroupID:   e.MuscleGroupID,
		MuscleGroupName: e.MuscleGroupName,
		Raw:             e.Raw,
	}
}

func decodeSet(raw json.RawMessage) (*model.Set, error) {
	var dto struct {
		ID              int             `json:"id"`
		Position        int             `json:"position"`
		SetType         *string         `json:"setType"`
		Status          *string         `json:"status"`
		Weight          *float64        `json:"weight"`
		WeightTarget    *float64        `json:"weightTarget"`
		WeightTargetMin *float64        `json:"weightTargetMin"`
		WeightTargetMax *float64        `json:"weightTargetMax"`
		Reps            *int            `json:"reps"`
		RepsTarget      json.RawMessage `json:"repsTarget"`
		Rir             *int            `json:"rir"`
		Bodyweight      *float64        `json:"bodyweight"`
		FinishedAt      *string         `json:"finishedAt"`
	}
	if err := decode(raw, &dto); err != nil {
		return nil, err
	}
	out := &model.Set{
		ID:              dto.ID,
		Position:        dto.Position,
		Status:          dto.Status,
		Weight:          dto.Weight,
		WeightTarget:    dto.WeightTarget,
		WeightTargetMin: dto.WeightTargetMin,
		WeightTargetMax: dto.WeightTargetMax,
		Reps:            dto.Reps,
		RepsTarget:      rawToString(dto.RepsTarget),
		Rir:             dto.Rir,
		Bodyweight:      dto.Bodyweight,
		FinishedAt:      parseTime(dto.FinishedAt),
		Raw:             raw,
	}
	if dto.SetType != nil {
		st := model.SetType(*dto.SetType)
		out.SetType = &st
	}
	return out, nil
}

// parseTime tolerates RP's RFC3339 variants (with/without fractional seconds, Z-less).
func parseTime(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, *s); err == nil {
			return &t
		}
	}
	return nil
}

// rawToString coerces a RawMessage that may be a JSON string OR a number
// into *string. RP's microRirs is sometimes "32108" (string) and sometimes
// 32108 (number); the API itself is inconsistent.
func rawToString(rm json.RawMessage) *string {
	if len(rm) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(rm, &s); err == nil {
		return &s
	}
	// fall through: leave the number as-is
	var n json.Number
	if err := json.Unmarshal(rm, &n); err == nil {
		s := n.String()
		return &s
	}
	return nil
}
