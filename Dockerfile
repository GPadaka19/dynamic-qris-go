FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o app .

FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata \
 && adduser -D -H -u 10001 app
WORKDIR /app

COPY --from=builder /src/app ./app
COPY --from=builder /src/web ./web

# `data/qris.jpg` dibaca saat runtime dan ikut di-bake ke image.
COPY --from=builder /src/data ./data
USER app

ENV PORT=4009
EXPOSE 4009
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null "http://127.0.0.1:${PORT}/healthz" || exit 1
CMD ["./app"]
