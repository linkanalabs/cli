COVER_MIN := 95.0

.PHONY: build test cover lint fmt vet run dev tidy update-manifest

build:
	go build -o lk ./cmd/lk

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

# Refresh the vendored CLI manifest from the Rails repo root. Downloads to a
# temp file and sanity-checks the JSON shape first, so a 404 (manifest not
# committed yet), a flaky fetch or a corrupt payload never clobbers the local
# copy.
update-manifest:
	@tmp=$$(mktemp); \
	if gh api repos/linkanalabs/linkana/contents/cli-manifest.json --jq .content 2>/dev/null | base64 -d > $$tmp 2>/dev/null \
		&& jq -e '.manifest_version >= 1 and (.endpoints | type == "array")' $$tmp > /dev/null 2>&1; then \
		mv $$tmp internal/manifest/cli-manifest.json; \
		echo "internal/manifest/cli-manifest.json updated from linkanalabs/linkana"; \
	else \
		rm -f $$tmp; \
		echo "error: could not fetch a valid cli-manifest.json from linkanalabs/linkana (404 or invalid JSON); local copy kept" >&2; \
		exit 1; \
	fi
