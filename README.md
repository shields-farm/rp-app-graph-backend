# rp-app-graph-backend

Read/write GraphQL gateway over the [RP Hypertrophy app](https://training.rpstrength.com)'s
undocumented REST API. Bring your own bearer token per request — the server
holds no credentials of its own.

## Why

`api.rpstrength.com` is a perfectly serviceable read API but every consumer
ends up re-writing the same client: gzip-on-write, app-proxy host differences,
the partial-priorities 500 trap, the Accept-Version pinning. This service is
that client — once — exposed as GraphQL so anything in the cluster (or any
other language) can use it without learning the REST quirks.

## Quickstart

```bash
# Build & run locally
go build -o rp-app-graph-backend ./cmd/server
RP_USER_ID=<your-rp-user-id> ./rp-app-graph-backend

# In docker
docker build -t rp-app-graph-backend .
docker run -e RP_USER_ID=<your-rp-user-id> -p 8080:8080 rp-app-graph-backend
```

Enable the playground:

```bash
PLAYGROUND=true RP_USER_ID=<your-rp-user-id> ./rp-app-graph-backend
# open http://localhost:8080/
```

## Auth

Every request needs your own RP bearer token (the `rp_token` cookie value you
get after signing into training.rpstrength.com):

```
Authorization: Bearer <rp_token>
```

The server never stores the token. It goes upstream to RP on behalf of the
request and is gone.

If you want different write paths to hit different user accounts, set the
header `X-RP-User-Id: <numeric RP id>` per request, or set a default via
`RP_USER_ID`. Without either, writes that need a user id return an error.

## Endpoints

* `POST /graphql` — the graph
* `GET /graphql` — same, query params
* `GET /healthz` — liveness
* `GET /` — gqlgen playground when `PLAYGROUND=true`

## Schema

The graph mirrors RP's shape: `Mesocycle → Week → Day → DayExercise → Set`,
plus templates, user attributes, exercise history. Every entity carries a
`raw: JSON` field with the exact upstream payload so nothing is lost.

Reads:

```graphql
query {
  mesocycles { key name weekCount createdAt }
  mesocycle(find: "25") {
    name key microRirs
    priorities { muscleGroupName tier }
    weeks { position days { label bodyweight exercises { exercise { name } sets { weight reps status } } } }
  }
}
```

Writes:

```graphql
mutation {
  setPriorities(mesoKey: "acd2rb6oflhz", changes: [
    { muscleGroup: "glutes",    tier: emphasize }
    { muscleGroup: "hamstrings", tier: grow }
  ]) { muscleGroupName tier }
}
```

## Notes on the upstream API

* Writes go to `training.rpstrength.com/api/...` (the app proxy), reads to
  `api.rpstrength.com`. The Python client this is ported from
  (`rp_api.py`) documents why.
* Write bodies are gzip-compressed. The priorities endpoint silently 500s
  on partial payloads — `setPriorities` fetches the current map and submits
  the merged full map server-side, the same trick the Python client uses.
* Sets have no `actualRir` field on the wire. RP exposes *target* RIR only,
  via `microRirs` on the mesocycle (one digit per week; trailing `8` is the
  deload marker, not "8 reps in reserve").

## Layout

```
cmd/server/        main entrypoint
internal/rp/       native Go client for api.rpstrength.com + app-proxy writes
internal/server/   HTTP plumbing (auth middleware, /healthz, mux)
graph/             gqlgen schema, model, resolvers
```

## Hacking

```bash
# regenerate the graph after editing graph/schema.graphqls — gqlgen merges
# hand-written resolver bodies into schema.resolvers.go, so regen is a no-op
# when the schema matches (CI enforces this)
go tool gqlgen generate

go vet ./...
go test ./...
```

Pre-commit runs `go vet`, `goimports`, and `gofmt -s`.
