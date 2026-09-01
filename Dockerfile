# Metis BPM — production image.
#
# Two stages, in this order for a reason: ui/embed.go carries `//go:embed
# all:dist`, so the Go build cannot even compile until ui/dist exists. That is
# also why the tree ships a placeholder there and .gitignore keeps the real
# output out — a clone has the file but not the assets.
#
# The UI is built by bun directly rather than by `metis --build-ui`, which
# shells out to bun itself: using the binary here would mean building the binary
# to build the UI to build the binary.

# ---- stage 1: the UI ----------------------------------------------------
FROM oven/bun:1-alpine AS ui

WORKDIR /src/ui
# Manifests first, so a source-only change does not re-resolve the dependency
# tree. --frozen-lockfile makes the image reproduce the lockfile rather than
# quietly drifting to newer versions at build time.
COPY ui/package.json ui/bun.lock* ./
# ui/scripts too: package.json declares a postinstall that bun runs during
# install, so without it the install fails outright. It exists to give
# typescript-eslint a compiler it can load — irrelevant to this image, which
# does not lint — but skipping scripts wholesale would also skip any dependency
# that legitimately needs one. Copying the directory keeps this install the same
# one a developer runs.
COPY ui/scripts ./scripts
RUN bun install --frozen-lockfile

COPY ui/ ./
# `bun run build` ends in the bundle budget check, so an image cannot be built
# from a tree that has blown it.
RUN bun run build

# ---- stage 2: the server ------------------------------------------------
FROM golang:1.27.0-alpine AS build

# git: the Go toolchain wants it for VCS stamping. ca-certificates: outbound
# connector calls are TLS, and scratch-adjacent bases carry no roots.
RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=ui /src/ui/dist ./ui/dist

# VERSION is the tag this image is built from; it lands in logs, traces and
# /healthz. The default stays "dev" rather than a fake number, because a trace
# labelled with a version that was never released is worse than one admitting it
# does not know.
ARG VERSION=dev
# CGO off: the image runs on a distroless base with no libc to link against, and
# every driver in use is pure Go.
ENV CGO_ENABLED=0
RUN go build -trimpath \
      -ldflags "-s -w -X github.com/gsoultan/metis/internal/app.version=${VERSION}" \
      -o /out/metis ./cmd/metis

# ---- stage 3: what actually ships ---------------------------------------
# distroless static: no shell, no package manager, nothing to exec if the
# process is ever made to run something it should not. The UI is embedded in the
# binary, so the image is one file plus certificates.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/metis /usr/local/bin/metis

# nonroot (uid 65532) is the base image's own user. The engine writes nothing to
# its filesystem in a server deployment — state is the database — so the root
# filesystem can be mounted read-only.
USER nonroot:nonroot

# 8080 HTTP (API + embedded UI), 8081 gRPC. Metrics default to loopback :9464
# and are deliberately not exposed; publish them explicitly via
# METIS_METRICS_ADDRESS if something needs to scrape across a pod network.
EXPOSE 8080 8081

# No HEALTHCHECK: the image has no shell or curl to run one with, and every
# orchestrator that matters prefers its own probe. Point it at /readyz — which
# checks the database — rather than /healthz, which deliberately does not.
ENTRYPOINT ["/usr/local/bin/metis"]
