# Metis BPM — the engine's production image.
#
# The engine only: the web app is gsoultan/metis-server, a separate image that
# serves the pages and forwards the browser's API calls here.

# ---- stage 1: the engine ------------------------------------------------
# Also the build platform, with Go cross-compiling to the target. CGO is off and
# every driver is pure Go, so this is a real cross-compile rather than
# emulation: an arm64 image builds at native speed instead of running the whole
# toolchain under QEMU.
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine AS build

# git: the Go toolchain wants it for VCS stamping. ca-certificates: outbound
# connector calls are TLS, and scratch-adjacent bases carry no roots.
RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# VERSION is the tag this image is built from; it lands in logs, traces and
# /healthz. The default stays "dev" rather than a fake number, because a trace
# labelled with a version that was never released is worse than one admitting it
# does not know.
ARG VERSION=dev
# TARGETARCH and TARGETOS are supplied by buildx per platform. Without them this
# stage would build for whatever the *builder* is and quietly ship an amd64
# binary inside an arm64 image — which fails at exec time, on the user's
# machine, with a message about a missing file.
ARG TARGETOS
ARG TARGETARCH

# CGO off: the image runs on a distroless base with no libc to link against, and
# every driver in use is pure Go. It is also what makes the cross-compile above
# free.
ENV CGO_ENABLED=0
ENV GOOS=${TARGETOS} GOARCH=${TARGETARCH}
RUN go build -trimpath \
      -ldflags "-s -w -X github.com/gsoultan/metis/internal/app.version=${VERSION}" \
      -o /out/metis ./cmd/metis

# ---- stage 2: what actually ships ---------------------------------------
# distroless static: no shell, no package manager, nothing to exec if the
# process is ever made to run something it should not. The image is one file
# plus certificates.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/metis /usr/local/bin/metis

# nonroot (uid 65532) is the base image's own user. The engine writes nothing to
# its filesystem in a server deployment — state is the database — so the root
# filesystem can be mounted read-only.
USER nonroot:nonroot

# 8080 HTTP (the API). gRPC is off unless METIS_GRPC_ADDRESS is set,
# and is not exposed: it applies none of the HTTP chain's authentication or
# limits. Metrics default to loopback :9464 and are deliberately not exposed
# either; publish them explicitly via METIS_METRICS_ADDRESS if something needs
# to scrape across a pod network.
EXPOSE 8080

# No HEALTHCHECK: the image has no shell or curl to run one with, and every
# orchestrator that matters prefers its own probe. Point it at /readyz — which
# checks the database — rather than /healthz, which deliberately does not.
ENTRYPOINT ["/usr/local/bin/metis"]
