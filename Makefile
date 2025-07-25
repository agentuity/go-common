.PHONY: all lint test vet tidy

all: test

lint:
	@echo "linting..."
	@go fmt ./...

vet:
	@echo "vetting..."
	@go vet ./...

tidy:
	@echo "tidying..."
	@go mod tidy

test: tidy lint vet
	@echo "testing..."
	@go test -v -count=1 ./...
