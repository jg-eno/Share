install:
	go fmt ./...
	go vet ./...
	go install

build:
	go build -o share

run:
	go run . serve