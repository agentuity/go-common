.PHONY: all lint test vet tidy vuln fuzz gen bench

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

vuln:
	@echo "checking for vulnerabilities..."
	@govulncheck -show verbose ./...

test: tidy lint vet vuln
	@echo "testing..."
	@go test -v -count=1 -race ./...
	@make fuzz
	@make bench

fuzz:
	@echo "fuzzing..."
	@go test -fuzz=FuzzEncryptDecrypt ./crypto -fuzztime=3s
	@go test -fuzz=FuzzDecryptMalformed ./crypto -fuzztime=3s
	@go test -fuzz=FuzzCorruptionPositions ./crypto -fuzztime=3s
	@go test -fuzz=FuzzPEMEncoding ./crypto -fuzztime=3s
	@go test -fuzz=FuzzStreamingPatterns ./crypto -fuzztime=3s
	@go test -fuzz=FuzzPartialCorruption ./crypto -fuzztime=3s
	@go test -fuzz=FuzzDifferentKeyPairs ./crypto -fuzztime=3s

bench:
	@echo "benchmarking..."
	@go test -bench=. -benchmem -v ./...

gen:
	@echo "generating..."
	@go generate ./... && go fmt ./...
