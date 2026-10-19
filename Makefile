# Shortcuts for macOS / Linux / WSL. Windows users can run the plain `go` commands
# shown in README.md instead; nothing here is required.

.PHONY: tidy run-api run-gateway test test-integration build up down

tidy:            ## download dependencies and write go.sum
	go mod tidy

run-api:         ## core API on :8080 (reads .env)
	go run ./cmd/api

run-gateway:     ## gateway on :8000 (reads .env)
	go run ./cmd/gateway

test:            ## unit tests (integration tests are skipped without TEST_DATABASE_URL)
	go vet ./... && go test ./...

# Needs a scratch Postgres; see README "Run the integration tests".
test-integration:
	TEST_DATABASE_URL="postgresql://postgres:devpass@localhost:5433/womads_test?sslmode=disable" go test ./... -count=1

build:
	go build -o bin/ ./cmd/...

up:
	docker compose up --build

down:
	docker compose down
