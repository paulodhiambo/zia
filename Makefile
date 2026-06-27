.PHONY: run-api run-worker run-cron migrate-up migrate-down lint test

run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

run-cron:
	go run ./cmd/cron

migrate-up:
	@echo "run: migrate -path migrations -database \"$$DATABASE_URL\" up"

migrate-down:
	@echo "run: migrate -path migrations -database \"$$DATABASE_URL\" down 1"

lint:
	golangci-lint run

test:
	go test ./...
