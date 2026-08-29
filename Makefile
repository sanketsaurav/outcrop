.PHONY: test server-dev plugin plugin-dev docker

test: ## Run server tests + plugin typecheck/build
	cd server && go vet ./... && go test ./...
	cd plugin && npm run build

server-dev: ## Run the server locally with throwaway config
	cd server && API_KEY=dev-key-0123456789abcdef BASE_URL=http://localhost:8080 \
		DATA_DIR=../.devdata go run ./cmd/outcrop

plugin: ## Production build of the Obsidian plugin
	cd plugin && npm run build

plugin-dev: ## Rebuild the plugin on change
	cd plugin && npm run dev

docker: ## Build the server Docker image
	docker build -t outcrop:dev server
