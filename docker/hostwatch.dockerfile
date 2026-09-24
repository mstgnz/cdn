# hostwatch is pure Go with no cgo, so it needs none of the ImageMagick build in
# the main dockerfile. The Go version matches the toolchain line in go.mod.
FROM golang:1.23.4-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY cmd/hostwatch ./cmd/hostwatch
COPY pkg/hostwatch ./pkg/hostwatch
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /hostwatch ./cmd/hostwatch

# distroless/static carries CA certificates for SMTP TLS and nothing else. It
# runs as uid 0, which compose strips of every capability: root-owned paths on
# the host stay readable by ownership, not by DAC override.
FROM gcr.io/distroless/static-debian12
COPY --from=build /hostwatch /hostwatch
ENTRYPOINT ["/hostwatch"]
