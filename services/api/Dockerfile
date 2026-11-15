# Veylo API — multi-stage build (Go)
FROM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/api ./cmd/api

FROM alpine:3.20

RUN addgroup -S veylo && adduser -S veylo -G veylo \
    && mkdir -p /data/uploads && chown -R veylo:veylo /data

COPY --from=build /out/api /usr/local/bin/api
COPY --from=build /src/migrations /app/migrations

USER veylo

WORKDIR /app

ENV MIGRATIONS_PATH=/app/migrations \
    STORAGE_PATH=/data/uploads

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --retries=10 \
  CMD wget -qO- http://localhost:8080/healthz || exit 1

ENTRYPOINT ["api"]
