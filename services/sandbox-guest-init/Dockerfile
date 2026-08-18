FROM golang:1.26.3-alpine AS builder

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/velane-sandbox-guest-init ./cmd/velane-sandbox-guest-init

# This image is an OCI artifact for injecting guest init into an immutable
# sandbox rootfs. It is not an invocation image and has no runtime entrypoint.
FROM scratch AS artifact

COPY --from=builder /out/velane-sandbox-guest-init /usr/local/bin/velane-sandbox-guest-init
