.PHONY: all lint test vet tidy vuln fuzz

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
	@go test -v -count=1 ./...
	@make fuzz

fuzz:
	@echo "fuzzing..."
	@go test -fuzz=FuzzEncryptDecrypt ./crypto -fuzztime=3s
	@go test -fuzz=FuzzDecryptMalformed ./crypto -fuzztime=3s
	@go test -fuzz=FuzzCorruptionPositions ./crypto -fuzztime=3s
	@go test -fuzz=FuzzPEMEncoding ./crypto -fuzztime=3s