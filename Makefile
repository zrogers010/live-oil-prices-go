.PHONY: all build build-frontend build-backend run dev clean deploy test test-go test-ts test-poller test-poller-extra test-poller-mutation test-poller-notbefore test-nginx-syntax test-move-legacy test-rollback test-deploy

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
	bash scripts/deploy.sh

test: test-go test-ts test-poller test-poller-extra test-poller-mutation test-poller-notbefore test-nginx-syntax test-move-legacy test-rollback test-deploy

test-go:
	go test ./...

test-ts:
	npm run test:types

test-poller:
	npx --no-install tsx web/src/poller.test.ts

test-poller-extra:
	npx --no-install tsx web/src/poller-extra.test.ts

test-poller-mutation:
	npx --no-install tsx web/src/poller-mutation.test.ts

test-poller-notbefore:
	npx --no-install tsx web/src/poller-notbefore.test.ts

test-nginx-syntax:
	bash scripts/test-nginx-syntax.sh

test-move-legacy:
	bash scripts/move-legacy-rollbacks_test.sh

test-rollback:
	bash scripts/rollback_test.sh

test-deploy:
	bash scripts/deploy_test.sh
