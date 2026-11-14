# The egress proxy: the one way out of a run's network.
#
# Built from this repository rather than pulled, because it is our own code --
# a proxy from a registry would filter on host names and could not re-resolve
# the address and refuse a private one, which is the half that stops a DNS
# answer from pointing an allowed name at Ballast's own network.

FROM golang:1.27-alpine AS build

WORKDIR /src

# The module files first, so a change to the code does not re-download the
# dependencies.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/egress-proxy ./cmd/egress-proxy
COPY internal/infra/egress ./internal/infra/egress
COPY internal/infra/netguard ./internal/infra/netguard
COPY internal/infra/logging ./internal/infra/logging

# Static, so the image below can hold nothing but this binary.
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /egress-proxy ./cmd/egress-proxy

# Scratch, not alpine: there is no shell, no package manager and no tooling in
# the one container a user's script can reach. Nothing to need certificates
# either -- the proxy tunnels TLS without terminating it, and forwards plain
# http. Docker writes /etc/resolv.conf at runtime, which is all the pure-Go
# resolver reads.
FROM scratch

COPY --from=build /egress-proxy /egress-proxy

# Not root, and not a user that exists in any image: there is no /etc/passwd
# here for it to be in.
USER 12345:12345

EXPOSE 3128

ENTRYPOINT ["/egress-proxy"]
