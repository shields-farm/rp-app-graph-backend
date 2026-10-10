// Package server assembles the HTTP handlers: GraphQL, health, and playground.
package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/shields-farm/rp-app-graph-backend/internal/rp"
)

type ctxKey string

const (
	ctxToken  ctxKey = "rp.token"
	ctxUserID ctxKey = "rp.userID"
)

// TokenFromContext returns the per-request RP bearer token injected by AuthMiddleware.
func TokenFromContext(ctx context.Context) (string, error) {
	t, ok := ctx.Value(ctxToken).(string)
	if !ok || t == "" {
		return "", errors.New("missing Authorization bearer token — pass your rp_token as 'Authorization: Bearer <token>'")
	}
	return t, nil
}

// UserIDFromContext returns the per-request RP user id, falling back to the server default.
func UserIDFromContext(ctx context.Context, defaultID int) int {
	if v, ok := ctx.Value(ctxUserID).(int); ok && v > 0 {
		return v
	}
	return defaultID
}

// AuthMiddleware extracts the RP bearer token. It does NOT hit RP's auth
// endpoint — it just forwards the credential. Missing or malformed tokens
// get a 401 with a hint; the actual auth check happens at the upstream call.
func AuthMiddleware(defaultUserID int, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if h == "" {
			http.Error(w, "missing Authorization header (expected 'Bearer <rp_token>')", http.StatusUnauthorized)
			return
		}
		const prefix = "Bearer "
		if !strings.HasPrefix(h, prefix) {
			http.Error(w, "Authorization header must be 'Bearer <rp_token>'", http.StatusUnauthorized)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
		if token == "" {
			http.Error(w, "empty Bearer token", http.StatusUnauthorized)
			return
		}

		userID := defaultUserID
		if hdr := r.Header.Get("X-RP-User-Id"); hdr != "" {
			if n, err := strconv.Atoi(strings.TrimSpace(hdr)); err == nil && n > 0 {
				userID = n
			}
		}

		ctx := context.WithValue(r.Context(), ctxToken, token)
		ctx = context.WithValue(ctx, ctxUserID, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// New assembles the HTTP mux.
//
//	gqlHandler: gqlgen's handler for the executable schema.
//	defaultUserID: when > 0 it's used for calls that don't supply X-RP-User-Id.
//	playgroundEnabled: when true, GET / serves the gqlgen playground pointing at /graphql.
func New(gqlHandler http.Handler, defaultUserID int, playgroundEnabled bool) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/graphql", AuthMiddleware(defaultUserID, gqlHandler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	if playgroundEnabled {
		mux.Handle("/", playground.Handler("rp-app-graph-backend", "/graphql"))
	}
	return mux
}

// NewRPClient builds the per-request client.
func NewRPClient(ctx context.Context, defaultUserID int) (*rp.Client, error) {
	token, err := TokenFromContext(ctx)
	if err != nil {
		return nil, err
	}
	uid := UserIDFromContext(ctx, defaultUserID)
	return rp.New(token, uid, nil), nil
}

var _ = errors.New // silence unused warnings when the file is partially edited
