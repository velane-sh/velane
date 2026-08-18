FROM golang:1.26.3-alpine AS builder

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/velane-sandbox-host-watchdog ./cmd/velane-sandbox-host-watchdog

# This image is an OCI artifact for an immutable sandbox-host AMI build. It is
# not an invocation image and must not be deployed as a service container.
FROM scratch AS artifact

COPY --from=builder /out/velane-sandbox-host-watchdog /usr/local/bin/velane-sandbox-host-watchdog
COPY systemd/velane-sandbox-host-watchdog.service /usr/lib/systemd/system/velane-sandbox-host-watchdog.service
COPY systemd/velane-sandbox-host-watchdog.tmpfiles.conf /usr/lib/tmpfiles.d/velane-sandbox-host-watchdog.conf
