# Build
FROM golang:1.22-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/grokbot ./cmd/grokbot

# Runtime
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
  && adduser -D -H -u 10001 grokbot
WORKDIR /app
COPY --from=build /out/grokbot /app/grokbot
COPY configs/config.example.yaml /app/configs/config.yaml
RUN mkdir -p /app/data && chown -R grokbot:grokbot /app
USER grokbot
ENV CONFIG_PATH=/app/configs/config.yaml \
    SQLITE_PATH=/app/data/grokbot.db
VOLUME ["/app/data"]
ENTRYPOINT ["/app/grokbot"]
