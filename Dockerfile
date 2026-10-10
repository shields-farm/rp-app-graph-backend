# Build stage
FROM golang:1.26-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# -trimpath removes local paths from panics; -ldflags strips DWARF.
# CGO_ENABLED=0 for a fully static binary that runs on any base.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags='-s -w' \
    -o /out/rp-app-graph-backend ./cmd/server

# Runtime stage: distroless static is the smallest that runs Go binaries
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/rp-app-graph-backend /rp-app-graph-backend
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
USER nonroot
EXPOSE 8080
ENV PORT=8080
ENTRYPOINT ["/rp-app-graph-backend"]
