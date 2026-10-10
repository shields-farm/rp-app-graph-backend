// Package graph contains the gqlgen resolver for the RP Hypertrophy backend.
package graph

import (
	"github.com/shields-farm/rp-app-graph-backend/internal/rp"
)

// Resolver is the gqlgen root.
type Resolver struct {
	// DefaultRPUserID is used when a request does not carry X-RP-User-Id.
	// Zero means "no default" — write operations will fail until callers
	// send the header.
	DefaultRPUserID int

	// RPClientFactory overrides the per-request client constructor. Tests
	// substitute a stub upstream here. Nil → default construction.
	RPClientFactory func(token string, userID int) *rp.Client
}

// DefaultUserID returns the configured fallback user id (0 = none).
func (r *Resolver) DefaultUserID() int { return r.DefaultRPUserID }
