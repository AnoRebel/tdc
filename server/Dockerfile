# Build the server.
#
# Built from the repository root so the build context includes the VERSION
# file: the binary must report the same version as every other artifact from
# this commit.
FROM golang:1.27-alpine AS build

# Build arguments are injected by the release workflow. They are empty in a
# local build, which is why buildinfo treats them as optional.
ARG VERSION=""
ARG COMMIT=""
ARG BUILD_TIME=""

WORKDIR /src

# Copy the module files first so dependency download is cached independently
# of source changes.
COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/ ./

# CGO is disabled so the result is a static binary that runs on a distroless
# or scratch base. -trimpath and the empty build ID keep builds reproducible.
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w -buildid= \
      -X github.com/AnoRebel/tdc/server/internal/buildinfo.Commit=${COMMIT} \
      -X github.com/AnoRebel/tdc/server/internal/buildinfo.BuildTime=${BUILD_TIME}" \
    -o /out/tdc-server ./cmd/tdc-server

# Run the server.
FROM alpine:3.21 AS runtime

# ca-certificates is needed for outbound TLS (federation, link previews,
# webhooks). tzdata so timestamps render in the operator's zone.
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 -h /home/tdc tdc \
    # Created here, owned by the unprivileged user, because Docker copies the
    # ownership of an image directory onto a fresh named volume mounted over
    # it. Without this the volume arrives owned by root and the server — which
    # deliberately does not run as root — cannot write an attachment to it.
    && mkdir -p /var/lib/tdc/attachments \
    && chown -R tdc:tdc /var/lib/tdc

COPY --from=build /out/tdc-server /usr/local/bin/tdc-server

# Runs unprivileged: the server needs no root capability, and an unprivileged
# container limits what a compromise reaches.
USER tdc
WORKDIR /home/tdc

EXPOSE 8080

# The health endpoint reports database reachability, so this detects a server
# that is running but cannot serve.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://127.0.0.1:8080/api/health || exit 1

ENTRYPOINT ["tdc-server"]
