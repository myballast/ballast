# The HTTP API.
#
# Built the same way as the egress proxy, and for the same reason: what ships is
# static binaries and nothing they could be talked into running.
#
# There are three. /api is the service. /migrate applies the schema and exits; the
# platform runs it before it starts a new version. It
# lives in this image because a pre-deploy command runs in the service's own
# image, and it is a binary rather than a tool because this image has no shell
# and no checkout to read the migrations from: they are embedded in it.
# /storagecheck proves the object store answers to the application's own
# credentials from where the API runs.

FROM golang:1.23-alpine AS build

WORKDIR /src

# The module files first, so a change to the code does not re-download the
# dependencies.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations

# Static, so the image below can hold nothing but this binary. The time zone
# database travels inside it -- cmd/api imports time/tzdata -- because an
# allowed window is resolved in its own zone and there is no /usr/share/zoneinfo
# in a scratch image.
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /api ./cmd/api
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /migrate ./cmd/migrate
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /storagecheck ./cmd/storagecheck

# Scratch, not alpine: no shell, no package manager, no tooling. Debugging a
# container is done from its logs and its endpoints.
FROM scratch

# Unlike the proxy, this one makes outbound TLS connections of its own: the
# identity providers for sign-in, the database, and whatever target a
# verification reads. Without these it would refuse every certificate.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

COPY --from=build /api /api
COPY --from=build /migrate /migrate
COPY --from=build /storagecheck /storagecheck

# Not root, and not a user that exists in any image: there is no /etc/passwd
# here for it to be in.
USER 12345:12345

# The default of BALLAST_ADDR. The platform may publish another port; this is
# documentation, not a binding.
EXPOSE 8080

ENTRYPOINT ["/api"]
