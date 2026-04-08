.PHONY: build build-linux build-mac run dev

build:
	go build -o paperless-aissist .

build-linux:
	GOOS=linux GOARCH=amd64 go build -o paperless-aissist-linux-amd64 .

build-mac:
	GOOS=darwin GOARCH=arm64 go build -o paperless-aissist-darwin-arm64 .

run:
	go run . --server

dev:
	go run . --server
