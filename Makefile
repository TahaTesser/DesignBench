.PHONY: fmt lint tidy

fmt:
@echo "Formatting Go sources"
@gofmt -w $(shell go list -f '{{.Dir}}' ./...)

lint:
@echo "Running golangci-lint"
@golangci-lint run ./...

tidy:
@echo "Tidying Go modules"
@go mod tidy
