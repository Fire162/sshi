BINARY_NAME=sshi
INSTALL_PATH=/usr/local/bin

.PHONY: all build install test clean

all: build

build:
	go build -ldflags="-s -w" -o $(BINARY_NAME) ./cmd/sshi

install: build
	install -m 755 $(BINARY_NAME) $(INSTALL_PATH)/$(BINARY_NAME)
	@echo "Installed $(BINARY_NAME) to $(INSTALL_PATH)/$(BINARY_NAME)"

test:
	go test -v -race ./...

clean:
	rm -f $(BINARY_NAME)
