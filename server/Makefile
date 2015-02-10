SHELL:= /bin/sh
DEPS:=$(CURDIR)/.godeps
GOPATH:=$(DEPS):$(CURDIR)
GO:= GOPATH=$(GOPATH) go

.PHONY: all

$(DEPS):
	$(GO) get

all: build

build: $(DEPS)
	$(GO) build test_server.go

test:
	$(GO) test -v .

run: $(DEPS)
	$(GO) run test_server.go

