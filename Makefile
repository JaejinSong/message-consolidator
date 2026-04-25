BINARY_NAME=message-consolidator
SERVICE_NAME=message-consolidator.service
INSTALL_DIR=/home/jinro/.gemini/message-consolidator

.PHONY: build run install-service uninstall-service status logs test-ui test-go test-ai test-all build-frontend build-backend build-all

build: build-all

build-frontend:
	@echo "Building Frontend (Vite)..."
	npm run build

build-backend:
	@echo "Building Backend (Go)..."
	# Why: Manual WhaTap instrumentation only (HTTP middleware in handlers/middleware_whatap.go +
	# explicit trace.StartWithContext for background goroutines). Auto-instrumentation
	# `whatap-go-inst` was removed because it failed to wrap gorilla/mux handlers,
	# leaving WhaTap with zero transaction visibility (verified 2026-04-25).
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY_NAME) .
	upx -1 $(BINARY_NAME)
	CGO_ENABLED=0 go build -ldflags="-s -w" -o mc-util ./cmd/mc-util
	upx -1 mc-util

build-all:
	@echo "Building FE and BE in parallel..."
	$(MAKE) -j2 build-frontend build-backend

run: build
	./$(BINARY_NAME)

install-service: build
	@echo "Installing systemd service (from scripts/vps/)..."
	sudo cp $(INSTALL_DIR)/scripts/vps/$(SERVICE_NAME) /etc/systemd/system/
	sudo systemctl daemon-reload
	sudo systemctl enable $(SERVICE_NAME)
	sudo systemctl restart $(SERVICE_NAME)
	@echo "Service installed and started."

uninstall-service:
	@echo "Stopping and removing service..."
	sudo systemctl stop $(SERVICE_NAME) || true
	sudo systemctl disable $(SERVICE_NAME) || true
	sudo rm /etc/systemd/system/$(SERVICE_NAME)
	sudo systemctl daemon-reload
	@echo "Service uninstalled."

status:
	systemctl status $(SERVICE_NAME)

logs:
	journalctl -u $(SERVICE_NAME) -n 100 -f

test-ui:
	npm test

test-go:
	go test ./...

# AI_SOURCES: 변경 시 regression 테스트를 트리거할 파일 패턴
AI_SOURCES := ai/prompts ai/prompts.go ai/gemini.go ai/executor.go ai/analyzers.go ai/rag.go

test-ai:
	@BASE=$$(git merge-base HEAD origin/main 2>/dev/null || echo "HEAD^"); \
	CHANGED=$$(git diff --name-only $$BASE HEAD -- $(AI_SOURCES); \
	           git diff --name-only -- $(AI_SOURCES)); \
	if [ -n "$$CHANGED" ]; then \
		echo "AI 변경 감지 ($$BASE 기준):"; \
		echo "$$CHANGED" | sed 's/^/  /'; \
		go test -v -tags regression ./ai/... ./tests/regression/...; \
	else \
		echo "AI 관련 변경 없음 — regression 테스트 생략 (강제 실행: make test-ai-force)"; \
	fi

test-ai-force:
	go test -v -tags regression ./ai/... ./tests/regression/...

test-all:
	@echo "Running all tests in parallel..."
	$(MAKE) -j3 test-go test-ai test-ui

sqlc-gen:
	@echo "Generating Go code from SQL queries..."
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate

clean:
	@echo "Cleaning up test artifacts..."
	rm -f test_*.txt
	rm -rf ai/testdata/prompt_cache/*.txt
	@echo "Cleanup complete."

.PHONY: build run install-service uninstall-service status logs test-ui test-go test-ai test-ai-force test-all build-frontend build-backend build-all sqlc-gen clean

