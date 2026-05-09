# texas-holdem server build automation.
#
# Common usage:
#   make                      # 本地平台编译 → ./texas-holdem-server
#   make dist                 # 交叉编译 + 打 tar.gz → dist/
#   make checksums            # SHA256SUMS for dist/
#   make release VERSION=v0.1.0
#                             # build + 上传到 GitHub Release
#   make test
#   make clean

BIN     := texas-holdem-server
PKG     := ./cmd/server
DIST    := dist

GO      ?= go
LDFLAGS := -s -w
GOFLAGS := -trimpath

GOSRC := $(shell find . -name '*.go' -not -path './$(DIST)/*' 2>/dev/null)

.PHONY: build dist linux-amd64 linux-arm64 archives checksums release test vet tidy clean help

# 默认目标：本地平台开发用编译
build: $(BIN)

$(BIN): $(GOSRC)
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $(BIN) $(PKG)
	@ls -lh $(BIN)

# 发布产物：两个 Linux 架构 + 各自打成 tar.gz + SHA256SUMS
dist: archives checksums

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

archives: $(DIST)/$(BIN)-linux-amd64.tar.gz $(DIST)/$(BIN)-linux-arm64.tar.gz

# 把二进制打包成 tar.gz。tar 内部只放二进制，不带目录前缀，便于 install.sh
# 直接 tar -xzf 后拿到 ./$(BIN)-linux-<arch>。
$(DIST)/$(BIN)-linux-amd64.tar.gz: $(DIST)/$(BIN)-linux-amd64
	tar -czf $@ -C $(DIST) $(BIN)-linux-amd64
	@ls -lh $@

$(DIST)/$(BIN)-linux-arm64.tar.gz: $(DIST)/$(BIN)-linux-arm64
	tar -czf $@ -C $(DIST) $(BIN)-linux-arm64
	@ls -lh $@

checksums: $(DIST)/SHA256SUMS

# SHA256 manifest 覆盖 tar.gz（install.sh 用它做完整性校验）
$(DIST)/SHA256SUMS: $(DIST)/$(BIN)-linux-amd64.tar.gz $(DIST)/$(BIN)-linux-arm64.tar.gz
	cd $(DIST) && shasum -a 256 $(BIN)-linux-*.tar.gz > SHA256SUMS
	@echo "---"
	@cat $@

# 发版：dist + 通过 gh CLI 创建 GitHub Release，上传 tar.gz + SHA256SUMS
#   make release VERSION=v0.1.0
VERSION ?=
release: dist
	@command -v gh >/dev/null 2>&1 || { \
		echo "需要 GitHub CLI: brew install gh && gh auth login" >&2; exit 1; }
	@[ -n "$(VERSION)" ] || { \
		echo "请传 VERSION，例如：make release VERSION=v0.1.0" >&2; exit 1; }
	gh release create $(VERSION) \
		--title "$(VERSION)" \
		--generate-notes \
		$(DIST)/$(BIN)-linux-amd64.tar.gz \
		$(DIST)/$(BIN)-linux-arm64.tar.gz \
		$(DIST)/SHA256SUMS

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...
	$(GO) build ./...

tidy:
	$(GO) mod tidy
	$(GO) mod verify

clean:
	rm -rf $(DIST) $(BIN)

help:
	@echo "Targets:"
	@echo "  build         本地平台编译（默认） → ./$(BIN)"
	@echo "  dist          交叉编译 + 打 tar.gz + 校验和 → $(DIST)/"
	@echo "  linux-amd64   只编 linux/amd64 二进制"
	@echo "  linux-arm64   只编 linux/arm64 二进制"
	@echo "  archives      只把已编译的二进制打成 tar.gz"
	@echo "  checksums     生成 $(DIST)/SHA256SUMS"
	@echo "  release       dist + 上传 GitHub Release（VERSION=v0.x.y, 需要 gh CLI）"
	@echo "  test          go test ./..."
	@echo "  vet           go vet + go build"
	@echo "  tidy          go mod tidy + verify"
	@echo "  clean         删除 $(DIST)/ 和本地二进制"
	@echo ""
	@echo "示例:"
	@echo "  make dist                       # 交叉编译两个架构并打包"
	@echo "  make release VERSION=v0.1.0     # 一条龙发布"
