# The worker, for self-hosting with compose.selfhost.yaml.
#
# Built the same way as the API and the egress proxy: a static binary in an
# empty image. It drives the Docker daemon through the socket it is given, and
# starts each run's generator and proxy as siblings of itself, not children.
#
# Built from the repository root, like the others:
#   docker build -f deploy/worker/Dockerfile -t ballast-worker .

FROM golang:1.23-alpine AS build

WORKDIR /src

# The module files first, so a change to the code does not re-download the
# dependencies.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

# Static, so the image below can hold nothing but this binary. The time zone
# database travels inside it -- cmd/worker imports time/tzdata -- because an
# allowed window is checked again here before a run starts, in its own zone.
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /worker ./cmd/worker

FROM scratch

# The worker pulls the k6 image from a registry when the daemon does not have
# it; the daemon makes that connection, but a worker that ever speaks TLS
# itself should not have to be rebuilt for it.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

COPY --from=build /worker /worker

# The user is set by compose.selfhost.yaml, not here: it has to own the run
# directory on the host and belong to the group of the Docker socket, and both
# depend on the machine. This default is only a non-root fallback.
USER 12345:12345

ENTRYPOINT ["/worker"]
