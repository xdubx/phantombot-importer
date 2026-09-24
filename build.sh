#!/bin/sh
# Builds Windows and Linux binaries into dist/ using Docker (no local Go needed).
set -e
docker run --rm -v "$PWD":/src -w /src -u "$(id -u):$(id -g)" -e HOME=/tmp -e CGO_ENABLED=0 golang:1.26 sh -c '
  go test ./... &&
  GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/phantombot-importer.exe . &&
  GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/phantombot-importer-linux .'
