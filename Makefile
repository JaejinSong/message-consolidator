BINARY_NAME=message-consolidator
SERVICE_NAME=message-consolidator.service
INSTALL_DIR=/home/jinro/.gemini/message-consolidator

.PHONY: build run install-service uninstall-service status logs test-ui

build:
	@echo "Minifying static files..."
	@# (Optional) go install github.com/tdewolff/minify/v2/cmd/minify@latest
	@if command -v minify > /dev/null; then \
		minify -r -o static-min/ static/; \
	else \
		echo "Warning: minify not found, skipping minification."; \
		cp -r static static-min; \
	fi
	CGO_ENABLED=0 whatap-go-inst go build -ldflags="-s -w" -o $(BINARY_NAME) .
	upx -1 $(BINARY_NAME)
	CGO_ENABLED=0 go build -ldflags="-s -w" -o mc-util ./cmd/mc-util
	upx -1 mc-util
	@rm -rf static-min

run: build
	./$(BINARY_NAME)

install-service: build
	@echo "Installing systemd service..."
	sudo cp $(INSTALL_DIR)/$(SERVICE_NAME) /etc/systemd/system/
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
