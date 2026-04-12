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
	CGO_ENABLED=0 whatap-go-inst go build -ldflags="-s -w" -o $(BINARY_NAME) .
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

test-ai:
	go test -tags regression ./ai/... ./tests/regression/...

test-all:
	@echo "Running all tests in parallel..."
	$(MAKE) -j3 test-go test-ai test-ui

sqlc-gen:
	@echo "Generating Go code from SQL queries..."
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate

