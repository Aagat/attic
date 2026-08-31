# syntax=docker/dockerfile:1.7

# Exact version tags keep the foundation image reproducible while allowing
# dependabot/renovate (or an operator) to make base-image updates explicit.
ARG GO_VERSION=1.24.6
ARG BUILD_ALPINE_VERSION=3.22
ARG RUNTIME_ALPINE_VERSION=3.22.1

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine${BUILD_ALPINE_VERSION} AS build

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

# The application is built as a static binary. No browser or PDF runtime is
# installed here: those dependencies belong to the later processing image.
RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

# Copy only the clean-room module inputs. In particular, do not COPY the
# repository root, which could put prototype source or tests in the image.
COPY cmd/attic ./cmd/attic
COPY internal ./internal
COPY migrations ./migrations

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -buildvcs=false -ldflags="-s -w" \
      -o /out/attic ./cmd/attic

FROM alpine:${RUNTIME_ALPINE_VERSION} AS runtime

LABEL org.opencontainers.image.title="Attic deployable foundation"
LABEL org.opencontainers.image.description="Clean-room Attic V1 foundation image; browser/PDF processing is not included yet"

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 attic \
    && adduser -S -D -H -u 10001 -G attic attic \
    && mkdir -p /data/artifacts /tmp/attic \
    && chown -R attic:attic /data /tmp/attic

COPY --from=build --chown=attic:attic /out/attic /usr/local/bin/attic
# The migration runner consumes a directory. Keep that directory in the
# runtime image, but make it immutable for the non-root process.
COPY --from=build --chown=attic:attic /src/migrations /app/migrations
RUN chmod -R a-w /app/migrations

ENV LISTEN_ADDRESS=0.0.0.0:8080 \
    ARTIFACT_ROOT=/data/artifacts \
    TMPDIR=/tmp/attic \
    HOME=/tmp/attic

WORKDIR /app
USER 10001:10001

VOLUME ["/data"]
EXPOSE 8080

# BusyBox wget is present in Alpine and does not need a writable filesystem.
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/health/ready || exit 1

ENTRYPOINT ["/usr/local/bin/attic"]
