# texas-holdem server build automation.
#
# Common usage:
#   make                      # build for current platform → ./texas-holdem-server
#   make dist                 # cross-compile linux/amd64 + linux/arm64 → dist/
#   make checksums            # SHA256SUMS for dist/
#   make release VERSION=v0.1.0
#                             # build + upload to GitHub Release via `gh` CLI
#   make test                 # go test ./...
#   make clean
#
# Cross-compilation is fully static (CGO_ENABLED=0). The binaries live in
# dist/ and follow GitHub release naming conventions so install.sh picks them
# up automatically.

BIN     := texas-holdem-server
PKG     := ./cmd/server
DIST    := dist

GO      ?= go
LDFLAGS := -s -w
GOFLAGS := -trimpath

# Find every Go file (incl. tests) for change detection. Excluding dist/ so the
# build output doesn't trigger itself in a re-run.
GOSRC := $(shell find . -name '*.go' -not -path './$(DIST)/*' 2>/dev/null)

.PHONY: build dist linux-amd64 linux-arm64 checksums release test vet tidy clean help

# Default target: developer iteration build for the current host platform.
build: $(BIN)

$(BIN): $(GOSRC)
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $(BIN) $(PKG)
	@ls -lh $(BIN)

# Cross-compile both Linux architectures for distribution.
dist: linux-amd64 linux-arm64

linux-amd64: $(DIST)/$(BIN)-linux-amd64
linux-arm64: $(DIST)/$(BIN)-linux-arm64

$(DIST)/$(BIN)-linux-amd64: $(GOSRC)
	@mkdir -p $(DIST)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
		$(GO) build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $@ $(PKG)
	@ls -lh $@

$(DIST)/$(BIN)-linux-arm64: $(GOSRC)
	@mkdir -p $(DIST)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
		$(GO) build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $@ $(PKG)
	@ls -lh $@

# SHA256 manifest. install.sh on the server can fetch this to verify downloads.
checksums: $(DIST)/SHA256SUMS

$(DIST)/SHA256SUMS: $(DIST)/$(BIN)-linux-amd64 $(DIST)/$(BIN)-linux-arm64
	cd $(DIST) && shasum -a 256 $(BIN)-* > SHA256SUMS
	@echo "---"
	@cat $@

# Publish a GitHub release. Needs the `gh` CLI authenticated once.
#   make release VERSION=v0.1.0
VERSION ?=
release: dist checksums
	@command -v gh >/dev/null 2>&1 || { \
		echo "需要 GitHub CLI: brew install gh && gh auth login" >&2; exit 1; }
	@[ -n "$(VERSION)" ] || { \
		echo "请传 VERSION，例如：make release VERSION=v0.1.0" >&2; exit 1; }
	gh release create $(VERSION) \
		--title "$(VERSION)" \
		--generate-notes \
		$(DIST)/$(BIN)-linux-amd64 \
		$(DIST)/$(BIN)-linux-arm64 \
		$(DIST)/SHA256SUMS

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...
	$(GO) build ./...

# Tidy + verify go.mod/go.sum.
tidy:
	$(GO) mod tidy
	$(GO) mod verify

clean:
	rm -rf $(DIST) $(BIN)

help:
	@echo "Targets:"
	@echo "  build         本地平台编译（默认） → ./$(BIN)"
	@echo "  dist          交叉编译 linux/amd64 + linux/arm64 → $(DIST)/"
	@echo "  linux-amd64   只编 linux/amd64"
	@echo "  linux-arm64   只编 linux/arm64"
	@echo "  checksums     生成 $(DIST)/SHA256SUMS"
	@echo "  release       dist + checksums + 上传到 GitHub Release"
	@echo "                需要：gh CLI 已登录、VERSION=v0.x.y"
	@echo "  test          go test ./..."
	@echo "  vet           go vet + go build"
	@echo "  tidy          go mod tidy + verify"
	@echo "  clean         删除 $(DIST)/ 和本地二进制"
	@echo ""
	@echo "示例:"
	@echo "  make dist                       # 出两个 Linux 二进制到 dist/"
	@echo "  make release VERSION=v0.1.0     # 一条龙发布"
