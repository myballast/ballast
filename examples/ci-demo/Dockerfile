# The demo service as a small image: one static binary and nothing else, run by nobody.
#
# In the repository of the example there is a go.mod. When this is built from the
# directory it comes from, in the repository of Ballast, there is not one, and the first
# build step makes it: the service uses the standard library alone.

FROM golang:1.23-alpine AS build
WORKDIR /src
COPY *.go ./
RUN [ -f go.mod ] || go mod init ci-demo
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /ci-demo .

FROM scratch
COPY --from=build /ci-demo /ci-demo
USER 65534:65534
EXPOSE 8080
ENTRYPOINT ["/ci-demo"]
