VERSION := $(shell git describe --tags --always --dirty)
BINARY  := desktop-event-hooks
CMD     := ./cmd/desktop-event-hooks

LDFLAGS := -ldflags="-X main.version=$(VERSION)"

.PHONY: build install

build:
	go build $(LDFLAGS) -o bin/$(BINARY) $(CMD)
install:
	go install $(LDFLAGS) $(CMD) 
