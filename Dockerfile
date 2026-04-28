FROM golang:1.23-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o app .

FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app

COPY --from=builder /src/app ./app
COPY --from=builder /src/web ./web

# `data/qris.jpg` dibaca saat runtime; foldernya disediakan agar bisa di-mount via volume.
RUN mkdir -p /app/data

ENV PORT=4009
EXPOSE 4009
CMD ["./app"]
