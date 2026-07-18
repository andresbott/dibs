COMMIT_SHA_SHORT ?= $(shell git rev-parse --short=12 HEAD)
PWD_DIR := ${CURDIR}

default: help

#==========================================================================================
##@ Testing
#==========================================================================================
test: ## run fast go tests
	@go test ./... -cover

lint: ## run go linter
	# depends on https://github.com/golangci/golangci-lint
	@golangci-lint run

license-check: ## check for invalid licenses
	# depends on https://github.com/elastic/go-licence-detector
	@go list -m -mod=readonly -json all | go-licence-detector -includeIndirect -rules allowedLicenses.json -overrides overrideLicenses.json

benchmark: ## run go benchmarks
	@go test -run=^$$ -bench=. ./...

COVERAGE_THRESHOLD ?= 70
.PHONY: coverage
coverage: ## check test coverage meets the threshold (app, internal and libs; metainfo excluded: linker-stamped vars only)
	@fail=0; \
	for pkg in $$(go list ./app/... ./internal/... ./libs/... 2>/dev/null | grep -v '/app/metainfo$$'); do \
		go test -coverprofile=coverage.out -covermode=atomic $$pkg > /dev/null; \
		if [ -f coverage.out ]; then \
			coverage=$$(go tool cover -func=coverage.out | grep total: | awk '{print $$3}' | sed 's/%//'); \
			if [ $$(echo "$$coverage < $(COVERAGE_THRESHOLD)" | bc -l) -eq 1 ]; then \
				echo "❌ Coverage in $$pkg is below $(COVERAGE_THRESHOLD)!"; \
				fail=1; \
			fi; \
			rm -f coverage.out; \
		else \
			echo "⚠️ No coverage data for $$pkg"; \
			fail=1; \
		fi; \
	done; \
	exit $$fail

.PHONY: e2e
e2e: ## run end-to-end binary lifecycle tests (builds a temp binary, requires rsync)
	@go test -tags e2e ./zarf/e2e/... -v -count=1

.PHONY: verify
verify: test license-check lint benchmark coverage e2e ## run the full verification suite

#==========================================================================================
##@ Running
#==========================================================================================
run: ## run the interactive profiles TUI (re-seeds a git-ignored dev config from zarf/sample each run, rewriting sample paths to this checkout)
	@sed 's|/path/to/dibs|$(CURDIR)|g' zarf/sample/config.yaml > dibs.dev.yaml
	@go run main.go --config dibs.dev.yaml

#==========================================================================================
##@ Building
#==========================================================================================
build: ## use goreleaser to build for the current OS/Arch
	# depends on https://goreleaser.com
	@goreleaser build --snapshot --clean --single-target

#==========================================================================================
##@ Release
#==========================================================================================
.PHONY: check-branch
check-branch:
	@current_branch=$$(git symbolic-ref --short HEAD) && \
	if [ "$$current_branch" != "main" ]; then \
		echo "Error: You are on branch '$$current_branch'. Please switch to 'main'."; \
		exit 1; \
	fi

.PHONY: check-git-clean
check-git-clean: # check if git repo is clean
	@git diff --quiet

tag: check-git-clean check-branch ## create and push a git tag to publish a new release
	@[ "${version}" ] || ( echo ">> version is not set, usage: make tag version=\"v1.2.3\" "; exit 1 )
	@git tag -d $(version) || true
	@git tag -a $(version) -m "Release version: $(version)"
	@git push --delete origin $(version) || true
	@git push origin $(version) || true

clean: ## clean build env
	@rm -rf dist

#==========================================================================================
#  Help
#==========================================================================================
.PHONY: help
help: # Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
