.PHONY: all build install

BINARY=qp
INSTALL_DIR=$(HOME)/bin

all: build

build:
	go build -o $(BINARY) .

install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY) $(INSTALL_DIR)/$(BINARY)