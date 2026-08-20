FROM golang:1.26.3-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/velane-sandbox-host-agent ./cmd/velane-sandbox-host-agent

# This image is an OCI artifact for an immutable sandbox-host AMI build. It is
# not an invocation image and must not be deployed as a service container.
FROM scratch AS artifact

COPY --from=builder /out/velane-sandbox-host-agent /usr/local/bin/velane-sandbox-host-agent
COPY systemd/velane-sandbox-host-agent.service /usr/lib/systemd/system/velane-sandbox-host-agent.service
