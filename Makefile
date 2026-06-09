install:
	go install -v ./cmd/monero-exporter

run:
	monero-exporter \
		--monero-addr=http://localhost:18081 \
		--bind-addr=:9000

test:
	go test ./...

lint:
	golangci-lint run ./...

# Build a snapshot release (binaries + archives) locally, without publishing.
# A real release is cut by pushing a v* tag — see .github/workflows/release.yml.
snapshot:
	goreleaser release --snapshot --clean

table-of-contents:
	doctoc --notitle ./README.md

.PHONY: install run test lint snapshot table-of-contents
