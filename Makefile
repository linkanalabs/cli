COVER_MIN := 95.0

.PHONY: build test cover lint fmt vet run dev tidy

build:
	go build ./...

test:
	go test -race ./...

cover:
	go test -race -coverprofile=coverage.out ./internal/...
	@total=$$(go tool cover -func=coverage.out | tail -1 | awk '{print $$3}' | tr -d '%'); \
	echo "total coverage: $$total% (min $(COVER_MIN)%)"; \
	awk "BEGIN{exit !($$total >= $(COVER_MIN))}" || { echo "coverage below $(COVER_MIN)%"; exit 1; }

lint:
	golangci-lint run

fmt:
	gofmt -w .

vet:
	go vet ./...

run:
	go run ./cmd/lk $(ARGS)

dev:
	LK_API_URL=http://localhost:3000 go run ./cmd/lk doctor

tidy:
	go mod tidy
