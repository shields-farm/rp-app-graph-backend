package graph

// Hand-written resolvers. gqlgen owns schema.resolvers.go (generated panic stubs);
// these live in a separate file and are bound because the method set satisfies the
// resolver interfaces gqlgen asserts against in generated.go. If the generated
// file regains its panic versions of the same method names after a regenerate,
// delete those panics — Go doesn't allow two methods with the same receiver+name.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/shields-farm/rp-app-graph-backend/graph/model"
	"github.com/shields-farm/rp-app-graph-backend/internal/rp"
	"github.com/shields-farm/rp-app-graph-backend/internal/server"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// errToGraphQL keeps RP's status code attached as an extension so callers
// can distinguish "your token is expired" (401) from "RP is down" (5xx).
func errToGraphQL(err error) error {
	if err == nil {
		return nil
	}
	if ae, ok := err.(*rp.APIError); ok {
		return &gqlerror.Error{
			Message:    ae.Error(),
			Extensions: map[string]any{"code": "RP_UPSTREAM", "status": ae.StatusCode},
		}
	}
	return err
}

// ─── Queries ─────────────────────────────────────────────────────────────────

func (r *queryResolver) Mesocycles(ctx context.Context) ([]*model.MesocycleSummary, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	mesos, err := c.ListMesocycles(ctx)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	out := make([]*model.MesocycleSummary, 0, len(mesos))
	for _, m := range mesos {
		out = append(out, &model.MesocycleSummary{
			ID:         m.ID,
			Key:        m.Key,
			Name:       m.Name,
			Status:     strPtr(m.Status),
			CreatedAt:  m.CreatedAt,
			FinishedAt: m.FinishedAt,
			WeekCount:  m.WeekCount,
		})
	}
	return out, nil
}

func (r *queryResolver) Mesocycle(ctx context.Context, key *string, find *string) (*model.Mesocycle, error) {
	if (key == nil || *key == "") && (find == nil || *find == "") {
		return nil, fmt.Errorf("mesocycle requires exactly one of key or find")
	}
	if key != nil && *key != "" && find != nil && *find != "" {
		return nil, fmt.Errorf("mesocycle requires exactly one of key or find — got both")
	}
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if key != nil && *key != "" {
		raw, err = c.GetMesocycle(ctx, *key)
	} else {
		raw, err = c.FindMesocycle(ctx, *find)
	}
	if err != nil {
		return nil, errToGraphQL(err)
	}
	return decodeMesocycleResolved(ctx, c, raw)
}

func (r *queryResolver) ActiveMesocycle(ctx context.Context) (*model.Mesocycle, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.ActiveMesocycle(ctx)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	return decodeMesocycleResolved(ctx, c, raw)
}

func (r *queryResolver) Priorities(ctx context.Context, mesoKey string) ([]*model.MuscleGroupPriority, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.GetMesocycle(ctx, mesoKey)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	meso, err := decodeMesocycle(raw)
	if err != nil {
		return nil, err
	}
	return meso.Priorities, nil
}

func (r *queryResolver) Exercises(ctx context.Context) ([]*model.Exercise, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.ListExercises(ctx)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	var list []exerciseDTO
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("parse exercises: %w", err)
	}
	out := make([]*model.Exercise, 0, len(list))
	for i := range list {
		out = append(out, list[i].toModel())
	}
	return out, nil
}

func (r *queryResolver) ExerciseHistory(ctx context.Context, exerciseID int) (json.RawMessage, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.ExerciseHistory(ctx, exerciseID)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	return raw, nil
}

func (r *queryResolver) Templates(ctx context.Context) ([]*model.Template, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.ListTemplates(ctx)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	out := make([]*model.Template, 0, len(rows))
	for _, row := range rows {
		var t struct {
			Key  string  `json:"key"`
			Name *string `json:"name"`
		}
		if err := json.Unmarshal(row, &t); err != nil {
			return nil, err
		}
		out = append(out, &model.Template{Key: t.Key, Name: t.Name, Raw: row})
	}
	return out, nil
}

func (r *queryResolver) Template(ctx context.Context, key string) (*model.Template, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.GetTemplate(ctx, key)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	var t struct {
		Key  string  `json:"key"`
		Name *string `json:"name"`
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	return &model.Template{Key: t.Key, Name: t.Name, Raw: raw}, nil
}

func (r *queryResolver) UserAttributes(ctx context.Context) (*model.UserAttributes, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.UserAttributes(ctx)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	return &model.UserAttributes{Raw: raw}, nil
}

func (r *queryResolver) UserExerciseHistory(ctx context.Context) (json.RawMessage, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.UserExerciseHistory(ctx)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	return raw, nil
}

func (r *queryResolver) Bootstrap(ctx context.Context) (json.RawMessage, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.Bootstrap(ctx)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	return raw, nil
}

// ─── Mutations ───────────────────────────────────────────────────────────────

func (r *mutationResolver) SetPriorities(ctx context.Context, mesoKey string, changes []*model.PriorityChange) ([]*model.MuscleGroupPriority, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	// Map to wire names. muscleGroup accepts id-as-string or alias.
	chg := map[string]string{}
	for _, p := range changes {
		if !p.Tier.IsValid() {
			return nil, fmt.Errorf("invalid tier %q", p.Tier)
		}
		chg[p.MuscleGroup] = string(p.Tier)
	}
	raw, err := c.SetPriorities(ctx, mesoKey, chg)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	// The PUT returns the meso detail again; surface priorities from it
	m, err := decodeMesocycle(raw)
	if err != nil {
		return nil, err
	}
	return m.Priorities, nil
}

func (r *mutationResolver) CreateMesocycle(ctx context.Context, payload json.RawMessage) (*model.Mesocycle, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.CreateMesocycle(ctx, payload)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	return decodeMesocycle(raw)
}

func (r *mutationResolver) UpdateMesocycle(ctx context.Context, key string, payload json.RawMessage) (*model.Mesocycle, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.UpdateMesocycle(ctx, key, payload)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	return decodeMesocycle(raw)
}

func (r *mutationResolver) DeleteMesocycle(ctx context.Context, key string) (bool, error) {
	c, err := r.client(ctx)
	if err != nil {
		return false, err
	}
	if err := c.DeleteMesocycle(ctx, key); err != nil {
		return false, errToGraphQL(err)
	}
	return true, nil
}

func (r *mutationResolver) AddMicro(ctx context.Context, mesoKey string) (*model.Mesocycle, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.AddMicro(ctx, mesoKey)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	return decodeMesocycle(raw)
}

func (r *mutationResolver) RemoveMicro(ctx context.Context, mesoKey string) (bool, error) {
	c, err := r.client(ctx)
	if err != nil {
		return false, err
	}
	if err := c.RemoveMicro(ctx, mesoKey); err != nil {
		return false, errToGraphQL(err)
	}
	return true, nil
}

func (r *mutationResolver) UpdateDay(ctx context.Context, dayID int, payload json.RawMessage) (*model.Day, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.UpdateDay(ctx, dayID, payload)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	return decodeDay(raw)
}

func (r *mutationResolver) UpdateDayBodyweight(ctx context.Context, dayID int, bodyweight float64) (*model.Day, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.UpdateDayBodyweight(ctx, dayID, bodyweight)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	return decodeDay(raw)
}

func (r *mutationResolver) UpdateDayLabel(ctx context.Context, dayID int, label string) (*model.Day, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.UpdateDayLabel(ctx, dayID, label)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	return decodeDay(raw)
}

func (r *mutationResolver) AddDayNote(ctx context.Context, dayID int, text string) (json.RawMessage, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.AddDayNote(ctx, dayID, text)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	return raw, nil
}

func (r *mutationResolver) AddExerciseToDay(ctx context.Context, dayID int, exercise model.DayExerciseInput) (*model.DayExercise, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	pos := 0
	if exercise.Position != nil {
		pos = *exercise.Position
	}
	raw, err := c.AddExerciseToDay(ctx, dayID, exercise.ExerciseID, exercise.MuscleGroupID, pos)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	return decodeDayExercise(raw)
}

func (r *mutationResolver) UpdateDayExercise(ctx context.Context, dayID int, dayExerciseID int, payload json.RawMessage) (*model.DayExercise, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.UpdateDayExercise(ctx, dayID, dayExerciseID, payload)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	return decodeDayExercise(raw)
}

func (r *mutationResolver) DeleteDayExercise(ctx context.Context, dayID int, dayExerciseID int) (bool, error) {
	c, err := r.client(ctx)
	if err != nil {
		return false, err
	}
	if err := c.DeleteDayExercise(ctx, dayID, dayExerciseID); err != nil {
		return false, errToGraphQL(err)
	}
	return true, nil
}

func (r *mutationResolver) UpdateSet(ctx context.Context, setID int, changes model.SetUpdateInput) (*model.Set, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{}
	if changes.Weight != nil {
		payload["weight"] = *changes.Weight
	}
	if changes.Reps != nil {
		payload["reps"] = *changes.Reps
	}
	if changes.Rir != nil {
		payload["rir"] = *changes.Rir
	}
	if changes.Status != nil {
		payload["status"] = *changes.Status
	}
	if changes.WeightTarget != nil {
		payload["weightTarget"] = *changes.WeightTarget
	}
	if changes.RepsTarget != nil {
		payload["repsTarget"] = *changes.RepsTarget
	}
	if changes.SetType != nil {
		payload["setType"] = changes.SetType.WireValue()
	}
	if changes.ProgressiveOverload != nil {
		payload["progressiveOverload"] = *changes.ProgressiveOverload
	}
	if len(changes.Extra) > 0 {
		var extra map[string]any
		if err := json.Unmarshal(changes.Extra, &extra); err != nil {
			return nil, fmt.Errorf("extra must be a JSON object: %w", err)
		}
		for k, v := range extra {
			payload[k] = v
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	raw, err := c.UpdateSet(ctx, setID, body)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	return decodeSet(raw)
}

func (r *mutationResolver) DeleteSet(ctx context.Context, setID int) (bool, error) {
	c, err := r.client(ctx)
	if err != nil {
		return false, err
	}
	if err := c.DeleteSet(ctx, setID); err != nil {
		return false, errToGraphQL(err)
	}
	return true, nil
}

func (r *mutationResolver) CreateTemplate(ctx context.Context, payload json.RawMessage) (*model.Template, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.CreateTemplate(ctx, payload)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	var t struct {
		Key  string  `json:"key"`
		Name *string `json:"name"`
	}
	_ = json.Unmarshal(raw, &t)
	return &model.Template{Key: t.Key, Name: t.Name, Raw: raw}, nil
}

func (r *mutationResolver) UpdateTemplate(ctx context.Context, key string, payload json.RawMessage) (*model.Template, error) {
	c, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.UpdateTemplate(ctx, key, payload)
	if err != nil {
		return nil, errToGraphQL(err)
	}
	var t struct {
		Key  string  `json:"key"`
		Name *string `json:"name"`
	}
	_ = json.Unmarshal(raw, &t)
	return &model.Template{Key: t.Key, Name: t.Name, Raw: raw}, nil
}

func (r *mutationResolver) DeleteTemplate(ctx context.Context, key string) (bool, error) {
	c, err := r.client(ctx)
	if err != nil {
		return false, err
	}
	if err := c.DeleteTemplate(ctx, key); err != nil {
		return false, errToGraphQL(err)
	}
	return true, nil
}

// ─── plumbing ────────────────────────────────────────────────────────────────

type queryResolver struct{ *Resolver }
type mutationResolver struct{ *Resolver }

// Query exposes the generated Query resolver.
func (r *Resolver) Query() QueryResolver { return &queryResolver{r} }

// Mutation exposes the generated Mutation resolver.
func (r *Resolver) Mutation() MutationResolver { return &mutationResolver{r} }

// Silence unused-import warnings if anything above is later deleted.
var (
	_ = http.MethodGet
	_ = server.TokenFromContext
)

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
