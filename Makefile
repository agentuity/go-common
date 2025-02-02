.PHONY: all lint test vet tidy

all: test

lint:
	@go fmt ./...

vet:
	@go vet ./...

tidy:
	@go mod tidy

test: tidy lint vet
	@go test -v -count=1 ./...
