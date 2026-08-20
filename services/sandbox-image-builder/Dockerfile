FROM golang:1.26.3-alpine AS builder

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/velane-sandbox-image-builder ./cmd/velane-sandbox-image-builder

# This image supplies the builder binary to a dedicated, isolated image-build
# host. It is not an invocation image and does not grant build privileges.
FROM gcr.io/distroless/static-debian12

COPY --from=builder /out/velane-sandbox-image-builder /velane-sandbox-image-builder

ENTRYPOINT ["/velane-sandbox-image-builder"]
