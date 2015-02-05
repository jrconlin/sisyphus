SHELL:= /bin/sh
DEPS:=$(CURDIR)/.godeps
GOPATH:=$(DEPS):$(CURDIR)
GO:= GOPATH=$(GOPATH) go

.PHONY: all build

$(DEPS):
	$(GO) get

all: build

build: $(DEPS)
	$(GO) build test_server.go

test:
	$(GO) test -v .

