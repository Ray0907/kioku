.PHONY: build install clean release

# macOS 27 CLT SDK ships .tbd files this linker rejects ("unknown architecture"); pin 26 when present.
SDK26 := /Library/Developer/CommandLineTools/SDKs/MacOSX26.sdk
ifneq ($(wildcard $(SDK26)),)
export SDKROOT ?= $(SDK26)
endif

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
# Oldest macOS Go supports; otherwise clang targets the build machine's SDK (e.g. 15.0).
MACOS_MIN ?= 12.0

build:
	go build -tags sqlite_fts5 -ldflags "$(LDFLAGS)" -o kioku .

install:
	go install -tags sqlite_fts5 -ldflags "$(LDFLAGS)" .

# macOS only for now (Phase 1). cgo, so each arch is built with clang's -arch.
release:
	rm -rf dist && mkdir -p dist
	for arch in arm64 amd64; do \
	  clangarch=$$arch; [ $$arch = amd64 ] && clangarch=x86_64; \
	  MACOSX_DEPLOYMENT_TARGET=$(MACOS_MIN) CGO_ENABLED=1 GOOS=darwin GOARCH=$$arch \
	  CC="clang -arch $$clangarch -mmacosx-version-min=$(MACOS_MIN)" CGO_LDFLAGS="-mmacosx-version-min=$(MACOS_MIN)" \
	    go build -trimpath -tags sqlite_fts5 -ldflags "$(LDFLAGS)" -o dist/kioku . && \
	  cp LICENSE README.md dist/ && \
	  tar -C dist -czf dist/kioku_$(VERSION)_darwin_$$arch.tar.gz kioku LICENSE README.md && \
	  rm dist/kioku dist/LICENSE dist/README.md || exit 1; \
	done
	cd dist && shasum -a 256 *.tar.gz > SHA256SUMS

clean:
	rm -rf kioku dist
