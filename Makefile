.PHONY: run test vet fmt lint build

run:
	go run .

test:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

lint:
	golangci-lint run

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o app .
