.PHONY: build test test-race vet vet-race cover smoke fmt tidy clean install-deps skills-status

## build: Compile the marketing-os binary into ./bin/
build:
	go build -trimpath -o ./bin/marketing-os ./cmd/marketing-os

## test: Run all tests
test:
	go test ./... -count=1

## test-race: Run all tests with the race detector
test-race:
	go test -race ./... -count=1

## vet: Run go vet
vet:
	go vet ./...

## cover: Generate coverage report
cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -n 1

## smoke: Build and run skills status smoke test
smoke: build
	./bin/marketing-os --config ./config.example.yaml --json skills status

## fmt: Format all Go files
fmt:
	gofmt -w $$(git ls-files '*.go') $$(git ls-files --others --exclude-standard '*.go')

## tidy: Run go mod tidy
tidy:
	go mod tidy

## install-deps: Download Go modules
install-deps:
	go mod download

## skills-status: Verify pinned skills lock
skills-status:
	go run ./cmd/marketing-os --config ./config.example.yaml --json skills status

## clean: Remove build artifacts
clean:
	rm -rf ./bin coverage.out
