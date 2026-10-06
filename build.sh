#!/usr/bin/env sh
set -eu

cd "$(dirname "$0")"
output="${OUTPUT:-bin/margit}"

echo "==> fill log tags";  go run ./cmd/tagger
echo "==> check log tags"; go run ./cmd/tagger -check
echo "==> gofmt";          gofmt -w .
echo "==> vet";            go vet ./...
if [ "${1:-}" = "test" ]; then
	echo "==> test";       go test -race ./...
fi
echo "==> build";          go build -o "$output" .

echo "built $output"
