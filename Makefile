BINARY_NAME=message-consolidator
SERVICE_NAME=message-consolidator.service
INSTALL_DIR=/home/jinro/.gemini/message-consolidator

.PHONY: build run install-service uninstall-service status logs

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY_NAME) .
	upx -1 $(BINARY_NAME)

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
