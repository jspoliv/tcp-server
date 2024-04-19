run: build
	./bin/tcp-server

build:
	go build -o bin/tcp-server