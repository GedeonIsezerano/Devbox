.PHONY: build test lint clean

build:
	go build -o bin/dbx ./cmd/dbx
	go build -o bin/dbx-server ./cmd/dbx-server

test:
	go test ./... -v -race

lint:
	go vet ./...

clean:
	rm -rf bin/
