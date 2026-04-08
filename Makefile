.PHONY: build build-linux build-mac release run dev

VERSION ?= dev

build:
	go build -ldflags="-X main.version=$(VERSION)" -o paperless-aissist .

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
		-ldflags="-s -w -X main.version=$(VERSION)" \
		-o paperless-aissist-linux-amd64 .

build-mac:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build \
		-ldflags="-s -w -X main.version=$(VERSION)" \
		-o paperless-aissist-darwin-arm64 .

release: build-linux build-mac

run:
	go run . --server

dev:
	go run . --server
