.PHONY: test server-dev plugin plugin-dev docker hooks release-plugin release-server

test: ## Server: vet + tests · Plugin: typecheck, unit tests, lint, build
	cd server && test -z "$$(gofmt -l .)" && go vet ./... && go test ./...
	cd plugin && npm run test && npm run lint && npm run build

server-dev: ## Run the server locally with throwaway config
	cd server && API_KEY=dev-key-0123456789abcdef BASE_URL=http://localhost:8080 \
		DATA_DIR=../.devdata go run ./cmd/outcrop

plugin: ## Production build of the Obsidian plugin
	cd plugin && npm run build

plugin-dev: ## Rebuild the plugin on change
	cd plugin && npm run dev

docker: ## Build the server Docker image
	docker build -t outcrop:dev server

hooks: ## Enable the repo's pre-commit hooks (format + lint staged files)
	git config core.hooksPath .githooks
	chmod +x .githooks/*
	@echo "pre-commit hooks enabled"

release-plugin: ## make release-plugin V=0.2.0 — bump versions, commit, tag
	@test -n "$(V)" || (echo "usage: make release-plugin V=x.y.z" && exit 1)
	node scripts/bump-plugin-version.mjs $(V)
	git add manifest.json versions.json plugin/package.json
	git commit -m "Release plugin $(V)"
	git tag $(V)
	@echo "now push it: git push origin main --tags"

release-server: ## make release-server V=0.2.0 — tag a server/Docker release
	@test -n "$(V)" || (echo "usage: make release-server V=x.y.z" && exit 1)
	git tag server-v$(V)
	@echo "now push it: git push origin main --tags"
