# syntax=docker/dockerfile:1.7

# Build stage
FROM golang:1.27.4-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git

ARG GOPROXY=https://proxy.golang.org,direct
ARG GOSUMDB=sum.golang.org
ENV GOPROXY=${GOPROXY} \
    GOSUMDB=${GOSUMDB} \
    GODEBUG=http2client=0

# Copy go mod files
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    for i in 1 2 3; do \
      go mod download && break; \
      if [ "$i" = "3" ]; then exit 1; fi; \
      sleep 5; \
    done

# Copy source code
COPY . .

# Build the API and the durable media worker
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /out && \
    CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /out/server ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /out/worker ./cmd/worker

# Final stage
FROM alpine:3.22

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy both process binaries from the builder. The API remains the default;
# deployments run the worker from this same image with `./worker`.
COPY --from=builder /out/server ./server
COPY --from=builder /out/worker ./worker

# Copy migrations (if needed at runtime)
COPY --from=builder /app/db/migrations ./db/migrations

# Expose port
EXPOSE 8080

# Run the application
CMD ["./server"]
