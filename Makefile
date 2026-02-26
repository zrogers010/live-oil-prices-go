.PHONY: all build build-frontend build-backend run dev clean deploy

all: build

build: build-frontend build-backend

build-frontend:
	npm install
	npm run build

build-backend:
	go build -o bin/server ./cmd/server

build-prod: build-frontend
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/server ./cmd/server

run: build
	./bin/server

dev:
	@echo "Starting development servers..."
	@npm run dev &
	@go run ./cmd/server

clean:
	rm -rf bin/ node_modules/ web/static/js/app.js web/static/js/app.js.map web/static/js/detail.js web/static/js/detail.js.map

deploy:
	bash deploy.sh
