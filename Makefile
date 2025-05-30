TEST_PACKAGES := $(shell find ./tests/** -type d -not -path './tests')

lint: |
	@echo "running golangci-lint..."
	@golangci-lint run --config .golangci.yml
	@echo "running modernize..."
	@go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@v0.18.1 ./...
	@echo "done."

lint-fix: |
	@echo "running golangci-lint..."
	@golangci-lint run --config .golangci.yml --fix
	@echo "running modernize..."
	@go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@v0.18.1 --fix ./...
	@echo "running builtin generator..."
	@go generate ./builtin/gen
	@echo "done."

test: |
	# iterate over each test package and run tests
	@bash -c 'for pkg in $(TEST_PACKAGES); do \
    		echo "Running tests in $$pkg"; \
    		go test -v -count=1 $$pkg || break; \
    	done'
