.PHONY: verify

verify:
	go test ./...
	go vet ./...
	node --test web/tests/*.test.mjs
	npm --prefix web run typecheck
	npm --prefix web run build
	git diff --check
