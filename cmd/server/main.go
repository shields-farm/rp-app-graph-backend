// rp-app-graph-backend: read/write GraphQL gateway over RP Hypertrophy's REST API.
//
// Configuration comes from env. RP's bearer token is per-request — this process
// never persists it.
//
//	PORT             (default 8080)
//	RP_USER_ID       default RP user id for writes when the request does not
//	                 carry the X-RP-User-Id header. 0 / unset = read-only.
//	PLAYGROUND       "true" enables a GraphQL playground on GET /.
package main

import (
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/shields-farm/rp-app-graph-backend/graph"
	"github.com/shields-farm/rp-app-graph-backend/internal/server"
	"github.com/vektah/gqlparser/v2/ast"
)

func main() {
	port := 8080
	if v := os.Getenv("PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < 65536 {
			port = n
		}
	}
	defaultUID := 0
	if v := os.Getenv("RP_USER_ID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			defaultUID = n
		}
	}
	playgroundEnabled := os.Getenv("PLAYGROUND") == "true"

	srv := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: &graph.Resolver{
		DefaultRPUserID: defaultUID,
	}}))

	// Sensible defaults; nothing fancy.
	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})
	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000))
	srv.Use(extension.Introspection{})
	srv.Use(extension.AutomaticPersistedQuery{Cache: lru.New[string](100)})

	mux := server.New(srv, defaultUID, playgroundEnabled)

	httpServer := &http.Server{
		Addr:              net.JoinHostPort("0.0.0.0", strconv.Itoa(port)),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second, // long enough for slow upstream PUTs
		IdleTimeout:       90 * time.Second,
	}

	log.Printf("rp-app-graph-backend listening on :%d (playground=%t)", port, playgroundEnabled)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
}
