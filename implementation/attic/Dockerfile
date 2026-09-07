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

LABEL org.opencontainers.image.title="Attic"
LABEL org.opencontainers.image.description="Clean-room Attic V1 article processing runtime"

RUN apk add --no-cache ca-certificates chromium fontconfig font-noto font-noto-cjk ttf-dejavu \
    pandoc-cli texlive-xetex texmf-dist-xetex texmf-dist-latexextra texmf-dist-fontsrecommended texmf-dist-langchinese \
    && mktexlsr \
    && ln -s /usr/share/texmf-dist/fonts/opentype/public/lm /usr/share/fonts/latin-modern \
    && fc-cache -f \
    && addgroup -S -g 10001 attic \
    && adduser -S -D -H -u 10001 -G attic attic \
    && mkdir -p /data/artifacts \
    && chown -R attic:attic /data

RUN apk add --no-cache poppler-utils

COPY --from=build --chown=attic:attic /out/attic /usr/local/bin/attic
# The migration runner consumes a directory. Keep that directory in the
# runtime image, but make it immutable for the non-root process.
COPY --from=build --chown=attic:attic /src/migrations /app/migrations
RUN chmod -R a-w /app/migrations

ENV LISTEN_ADDRESS=0.0.0.0:8080 \
    ARTIFACT_ROOT=/data/artifacts \
    BROWSER_EXECUTABLE=/usr/bin/chromium-browser \
    TMPDIR=/tmp \
    HOME=/tmp

WORKDIR /app
USER 10001:10001

VOLUME ["/data"]
EXPOSE 8080

# BusyBox wget is present in Alpine and does not need a writable filesystem.
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/health/ready || exit 1

ENTRYPOINT ["/usr/local/bin/attic"]
