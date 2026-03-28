BINARY    := rewind.exe
CMD       := ./cmd/rewind
MANIFEST  := cmd/rewind/rewind.manifest
RC        := cmd/rewind/resource.rc
SYSO      := cmd/rewind/resource.syso
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS_COMMON := -X main.version=$(VERSION)

.PHONY: all debug release resource clean vet install uninstall

all: debug

## resource - compile manifest into a .syso for embedding (run when manifest changes)
resource: $(SYSO)

$(SYSO): $(MANIFEST) $(RC)
	windres -i $(RC) -o $(SYSO)

## debug - console window visible, full debug info, race detector
debug: $(SYSO)
	go build -race -ldflags "$(LDFLAGS_COMMON)" -o $(BINARY) $(CMD)

## release - no console window, stripped binary
release: $(SYSO)
	go build -ldflags "-H windowsgui -s -w $(LDFLAGS_COMMON)" -o $(BINARY) $(CMD)

## vet - run `go vet` and staticcheck if available
vet:
	go vet ./...
	@if command -v staticcheck >/dev/null 2>&1; then staticcheck ./...; fi

## clean - remove built binary and compiled resource
clean:
	rm -f $(BINARY) $(SYSO)

## install - copy release binary to %LOCALAPPDATA%\rewind\rewind.exe
install: release
	mkdir -p "$(LOCALAPPDATA)/rewind"
	cp $(BINARY) "$(LOCALAPPDATA)/rewind/$(BINARY)"
	@echo "Installed to $(LOCALAPPDATA)/rewind/$(BINARY)"

## uninstall - remove installed binary
uninstall:
	rm -f "$(LOCALAPPDATA)/rewind/$(BINARY)"

help:
	@grep -E '^##' Makefile | sed 's/## /  /'
