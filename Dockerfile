FROM --platform=${BUILDPLATFORM} golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS builder

ARG TARGETARCH

ENV CGO_ENABLED=0 \
    GOARCH=${TARGETARCH} \
    GOFLAGS="-ldflags=-s" \
    GOCACHE="/cache/build" \
    GOMODCACHE="/cache/mod"

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=${GOMODCACHE} \
    go mod download

COPY --parents cmd internal ./
RUN --mount=type=cache,target=${GOCACHE} \
    --mount=type=cache,target=${GOMODCACHE} \
    go build -o /bin/volkeep ./cmd/volkeep

FROM gcr.io/distroless/static@sha256:1a64f46364620465ef9e9a8f5413757dfdf22695e050bbfd7303eeaf17a59383 AS runtime

COPY --from=ghcr.io/tarampampam/microcheck:1.4.0@sha256:c9f79cd408626de7c10f2d487d67339f49adf0ba61dde96ede65343269db1f85 /bin/pidcheck /bin/pidcheck

COPY --from=builder /bin/volkeep /bin/volkeep

USER 65532:65532
HEALTHCHECK NONE

ENTRYPOINT ["/bin/volkeep"]
