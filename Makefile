.PHONY: all build install uninstall clean help test integration-test build-all llama-lib llama-lib-arm64 llama-lib-x86-64 build-native build-native-arm64 build-native-x86-64 nativebench-x86-64 localllm-integration-test-x86-64 run-native bench-native docker-build-native docker-save-native

# Build variables
BINARY_NAME=kuromatsu
BUILD_DIR=build
CMD_DIR=cmd/$(BINARY_NAME)
MAIN_GO=$(CMD_DIR)/main.go
EXT=

ifeq ($(OS),Windows_NT)
	POWERSHELL=powershell -NoProfile -Command
	WINDOWS_GOARCH_RAW:=$(strip $(shell go env GOARCH 2>NUL))
endif

# Version
ifeq ($(OS),Windows_NT)
	VERSION_RAW:=$(strip $(shell git describe --tags --always --dirty 2>NUL))
	GIT_COMMIT_RAW:=$(strip $(shell git rev-parse --short=8 HEAD 2>NUL))
	BUILD_TIME_RAW:=$(strip $(shell powershell -NoProfile -Command "Get-Date -Format 'yyyy-MM-ddTHH:mm:ssK'"))
	GO_VERSION_RAW:=$(strip $(shell go env GOVERSION 2>NUL))
else
	VERSION_RAW:=$(strip $(shell git describe --tags --always --dirty 2>/dev/null))
	GIT_COMMIT_RAW:=$(strip $(shell git rev-parse --short=8 HEAD 2>/dev/null))
	BUILD_TIME_RAW:=$(strip $(shell date +%FT%T%z))
	GO_VERSION_RAW:=$(strip $(shell go env GOVERSION 2>/dev/null))
endif
VERSION?=$(if $(VERSION_RAW),$(VERSION_RAW),dev)
GIT_COMMIT=$(if $(GIT_COMMIT_RAW),$(GIT_COMMIT_RAW),dev)
BUILD_TIME=$(if $(BUILD_TIME_RAW),$(BUILD_TIME_RAW),dev)
GO_VERSION=$(if $(GO_VERSION_RAW),$(firstword $(GO_VERSION_RAW)),unknown)
CONFIG_PKG=github.com/andre25costa-code/kuromatsu/pkg/config
LDFLAGS=-X $(CONFIG_PKG).Version=$(VERSION) -X $(CONFIG_PKG).GitCommit=$(GIT_COMMIT) -X $(CONFIG_PKG).BuildTime=$(BUILD_TIME) -X $(CONFIG_PKG).GoVersion=$(GO_VERSION) -s -w

# Go variables
GO?=go
CGO_ENABLED?=0
GO_BUILD_TAGS?=goolm,stdjson
GOFLAGS?=-v -tags $(GO_BUILD_TAGS)
GOCACHE?=$(CURDIR)/.cache/go-build
GOMODCACHE?=$(CURDIR)/.cache/go-mod
GOTOOLCHAIN?=local
export CGO_ENABLED
export GOCACHE
export GOMODCACHE
export GOTOOLCHAIN
comma:=,
empty:=
space:=$(empty) $(empty)
GO_BUILD_TAGS_NO_GOOLM:=$(subst $(space),$(comma),$(strip $(filter-out goolm,$(subst $(comma),$(space),$(GO_BUILD_TAGS)))))
GOFLAGS_NO_GOOLM?=-v -tags $(GO_BUILD_TAGS_NO_GOOLM)

# Patch MIPS LE ELF e_flags (offset 36) for NaN2008-only kernels (e.g. Ingenic X2600).
#
# Bytes (octal): \004 \024 \000 \160  →  little-endian 0x70001404
#   0x70000000  EF_MIPS_ARCH_32R2   MIPS32 Release 2
#   0x00001000  EF_MIPS_ABI_O32     O32 ABI
#   0x00000400  EF_MIPS_NAN2008     IEEE 754-2008 NaN encoding
#   0x00000004  EF_MIPS_CPIC        PIC calling sequence
#
# Go's GOMIPS=softfloat emits no FP instructions, so the NaN mode is irrelevant
# at runtime — this is purely an ELF metadata fix to satisfy the kernel's check.
# patchelf cannot modify e_flags; dd at a fixed offset is the most portable way.
#
# Ref: https://codebrowser.dev/linux/linux/arch/mips/include/asm/elf.h.html
define PATCH_MIPS_FLAGS
	@if [ -f "$(1)" ]; then \
		printf '\004\024\000\160' | dd of=$(1) bs=1 seek=36 count=4 conv=notrunc 2>/dev/null || \
		{ echo "Error: failed to patch MIPS e_flags for $(1)"; exit 1; }; \
	else \
		echo "Error: $(1) not found, cannot patch MIPS e_flags"; exit 1; \
	fi
endef

# Patch creack/pty for loong64 support (upstream doesn't have ztypes_loong64.go)
PTY_PATCH_LOONG64=pty_dir=$$(go env GOMODCACHE)/github.com/creack/pty@v1.1.9; \
	if [ -d "$$pty_dir" ] && [ ! -f "$$pty_dir/ztypes_loong64.go" ]; then \
		chmod +w "$$pty_dir" 2>/dev/null || true; \
		printf '//go:build linux && loong64\npackage pty\ntype (_C_int int32; _C_uint uint32)\n' > "$$pty_dir/ztypes_loong64.go"; \
	fi

# Golangci-lint
GOLANGCI_LINT?=golangci-lint

# Installation
INSTALL_PREFIX?=$(HOME)/.local
INSTALL_BIN_DIR=$(INSTALL_PREFIX)/bin
INSTALL_MAN_DIR=$(INSTALL_PREFIX)/share/man/man1
INSTALL_TMP_SUFFIX=.new

# Workspace and Skills
PICOCLAW_HOME?=$(HOME)/.picoclaw
WORKSPACE_DIR?=$(PICOCLAW_HOME)/workspace
WORKSPACE_SKILLS_DIR=$(WORKSPACE_DIR)/skills
BUILTIN_SKILLS_DIR=$(CURDIR)/skills

LNCMD=ln -sf

# OS detection
ifeq ($(OS),Windows_NT)
	UNAME_S=Windows
	ifeq ($(WINDOWS_GOARCH_RAW),amd64)
		UNAME_M=x86_64
	else ifeq ($(WINDOWS_GOARCH_RAW),arm64)
		UNAME_M=arm64
	else ifeq ($(WINDOWS_GOARCH_RAW),386)
		UNAME_M=x86
	else
		UNAME_M=$(if $(WINDOWS_GOARCH_RAW),$(WINDOWS_GOARCH_RAW),x86_64)
	endif
else
	UNAME_S?=$(shell uname -s)
	UNAME_M?=$(shell uname -m)
endif

# Platform-specific settings
ifeq ($(UNAME_S),Linux)
	PLATFORM=linux
	ifeq ($(UNAME_M),x86_64)
		ARCH=amd64
	else ifeq ($(UNAME_M),aarch64)
		ARCH=arm64
	else ifeq ($(UNAME_M),armv81)
		ARCH=arm64
	else ifeq ($(UNAME_M),loongarch64)
		ARCH=loong64
	else ifeq ($(UNAME_M),riscv64)
		ARCH=riscv64
	else ifeq ($(UNAME_M),mipsel)
		ARCH=mipsle
	else
		ARCH=$(UNAME_M)
	endif
else ifeq ($(UNAME_S),Darwin)
	PLATFORM=darwin
	ifeq ($(UNAME_M),x86_64)
		ARCH?=amd64
	else ifeq ($(UNAME_M),arm64)
		ARCH?=arm64
	else
		ARCH?=$(UNAME_M)
	endif
else
	PLATFORM=$(UNAME_S)
	ifeq ($(UNAME_M),x86_64)
		ARCH?=amd64
	else
	    ARCH?=$(UNAME_M)
	endif
	# Detect Windows (Git Bash / MSYS2)
    IS_WINDOWS:=$(if $(findstring MINGW,$(UNAME_S)),yes,$(if $(findstring MSYS,$(UNAME_S)),yes,$(if $(findstring CYGWIN,$(UNAME_S)),yes,no)))
	ifeq ($(IS_WINDOWS),yes)
	    EXT=.exe
	    LNCMD=cp
	else ifeq ($(UNAME_S),windows) # failsafe for force windows build in other OS using UNAME_S=windows
		EXT=.exe
	endif

endif

ifeq ($(OS),Windows_NT)
	PLATFORM=windows
	ifeq ($(UNAME_M),x86_64)
		ARCH?=amd64
	else ifeq ($(UNAME_M),arm64)
		ARCH?=arm64
	else
		ARCH?=$(UNAME_M)
	endif
	EXT=.exe
endif

ifneq ($(strip $(GOOS)),)
	PLATFORM:=$(GOOS)
endif

ifneq ($(strip $(GOARCH)),)
	ARCH:=$(GOARCH)
endif

ifeq ($(PLATFORM),windows)
	EXT=.exe
endif

BINARY_PATH=$(BUILD_DIR)/$(BINARY_NAME)-$(PLATFORM)-$(ARCH)

# Default target
all: build

## generate: Run generate
generate:
	@echo "Run generate..."
ifeq ($(OS),Windows_NT)
	@$(POWERSHELL) "if (Test-Path -LiteralPath './$(CMD_DIR)/workspace') { Remove-Item -LiteralPath './$(CMD_DIR)/workspace' -Recurse -Force }"
	@$(POWERSHELL) "$$env:GOOS=''; $$env:GOARCH=''; $(GO) generate ./..."
else
	@rm -r ./$(CMD_DIR)/workspace 2>/dev/null || true
	@GOOS=$$($(GO) env GOHOSTOS) GOARCH=$$($(GO) env GOHOSTARCH) $(GO) generate ./...
endif
	@echo "Run generate complete"

## build: Build the kuromatsu binary for current platform
build: generate
	@echo "Building $(BINARY_NAME)$(EXT) for $(PLATFORM)/$(ARCH)..."
ifeq ($(OS),Windows_NT)
	@$(POWERSHELL) "New-Item -ItemType Directory -Force -Path '$(BUILD_DIR)' | Out-Null"
	@$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY_PATH)$(EXT) ./$(CMD_DIR)
	@$(POWERSHELL) "Copy-Item -LiteralPath '$(BINARY_PATH)$(EXT)' -Destination '$(BUILD_DIR)/$(BINARY_NAME)$(EXT)' -Force"
else
	@mkdir -p $(BUILD_DIR)
	@GOOS=$(PLATFORM) GOARCH=$(ARCH) $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY_PATH)$(EXT) ./$(CMD_DIR)
	@echo "Build complete: $(BINARY_PATH)$(EXT)"
	@$(LNCMD) $(BINARY_NAME)-$(PLATFORM)-$(ARCH)$(EXT) $(BUILD_DIR)/$(BINARY_NAME)$(EXT)
endif
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)$(EXT)"

## build-whatsapp-native: Build with WhatsApp native (whatsmeow) support; larger binary
build-whatsapp-native: generate
## @echo "Building $(BINARY_NAME) with WhatsApp native for $(PLATFORM)/$(ARCH)..."
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build -tags $(GO_BUILD_TAGS),whatsapp_native -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./$(CMD_DIR)
	GOOS=linux GOARCH=arm GOARM=7 $(GO) build -tags $(GO_BUILD_TAGS),whatsapp_native -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm ./$(CMD_DIR)
	GOOS=linux GOARCH=arm64 $(GO) build -tags $(GO_BUILD_TAGS),whatsapp_native -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./$(CMD_DIR)
	GOOS=linux GOARCH=loong64 $(GO) build -tags $(GO_BUILD_TAGS),whatsapp_native -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-loong64 ./$(CMD_DIR)
	GOOS=linux GOARCH=riscv64 $(GO) build -tags $(GO_BUILD_TAGS),whatsapp_native -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-riscv64 ./$(CMD_DIR)
	GOOS=linux GOARCH=mipsle GOMIPS=softfloat $(GO) build -tags $(GO_BUILD_TAGS_NO_GOOLM),whatsapp_native -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-mipsle ./$(CMD_DIR)
	$(call PATCH_MIPS_FLAGS,$(BUILD_DIR)/$(BINARY_NAME)-linux-mipsle)
	GOOS=darwin GOARCH=arm64 $(GO) build -tags $(GO_BUILD_TAGS),whatsapp_native -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./$(CMD_DIR)
	GOOS=windows GOARCH=amd64 $(GO) build -tags $(GO_BUILD_TAGS),whatsapp_native -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./$(CMD_DIR)
## @$(GO) build $(GOFLAGS) -tags whatsapp_native -ldflags "$(LDFLAGS)" -o $(BINARY_PATH) ./$(CMD_DIR)
	@echo "Build complete"
##	@ln -sf $(BINARY_NAME)-$(PLATFORM)-$(ARCH) $(BUILD_DIR)/$(BINARY_NAME)

## build-linux-arm: Build for Linux ARMv7 (e.g. Raspberry Pi Zero 2 W 32-bit)
build-linux-arm: generate
	@echo "Building for linux/arm (GOARM=7)..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm GOARM=7 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm ./$(CMD_DIR)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)-linux-arm"

## build-linux-arm64: Build for Linux ARM64 (e.g. Raspberry Pi Zero 2 W 64-bit)
build-linux-arm64: generate
	@echo "Building for linux/arm64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./$(CMD_DIR)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64"

## build-linux-mipsle: Build for Linux MIPS32 LE
build-linux-mipsle: generate
	@echo "Building for linux/mipsle (softfloat)..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=mipsle GOMIPS=softfloat $(GO) build $(GOFLAGS_NO_GOOLM) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-mipsle ./$(CMD_DIR)
	$(call PATCH_MIPS_FLAGS,$(BUILD_DIR)/$(BINARY_NAME)-linux-mipsle)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)-linux-mipsle"

## build-android-arm64: Build core for Android ARM64
build-android-arm64: generate
	@echo "Building for android/arm64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=android GOARCH=arm64 $(GO) build -tags stdjson -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-android-arm64 ./$(CMD_DIR)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)-android-arm64"

## build-pi-zero: Build for Raspberry Pi Zero 2 W (32-bit and 64-bit)
build-pi-zero: build-linux-arm build-linux-arm64
	@echo "Pi Zero 2 W builds: $(BUILD_DIR)/$(BINARY_NAME)-linux-arm (32-bit), $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 (64-bit)"

# ---- Native llama.cpp inference (build tag: nativellm) ----
# Embeds the Bonsai-1.7B-Q1_0 GGUF's runtime (the PrismML "prism" fork of
# llama.cpp, git submodule at ./llama.cpp) directly in the binary via cgo,
# instead of talking to a separate llama-server process (ADR-001/002/003).
# `make build` above is untouched and stays CGO_ENABLED=0 pure Go; these
# targets are the only ones that need a C++ toolchain (S18, S32).
LLAMA_DIR=llama.cpp
LLAMA_BUILD_DIR=$(LLAMA_DIR)/build-native
# Separate build dirs per pinned-CPU variant: CMake fatal-errors if a cache
# configured for one compiler/flag set is reused with a different one, so
# running `make llama-lib` (host arch, auto-detect) alongside a pinned target
# in the same tree needs distinct directories, not a rm -rf dance.
LLAMA_BUILD_DIR_ARM64=$(LLAMA_DIR)/build-native-arm64
LLAMA_BUILD_DIR_X86_64=$(LLAMA_DIR)/build-native-x86-64
CMAKE?=cmake
# ggml/llama.cpp translation units are large C++; building all of them at
# full core count can spike host RAM well past what a dev machine has free
# (observed OOM at unlimited -j on a 12-core/8GB box). Override with
# `make llama-lib LLAMA_BUILD_JOBS=8` on a beefier machine, but never build
# this ON the 1GB-RAM deploy target itself -- see docker/Dockerfile.native
# (E7), which is the only place this is meant to run for a real deploy.
LLAMA_BUILD_JOBS?=3
LLAMA_CMAKE_COMMON=-DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF -DGGML_STATIC=ON \
	-DLLAMA_BUILD_TESTS=OFF -DLLAMA_BUILD_EXAMPLES=OFF -DLLAMA_BUILD_TOOLS=OFF \
	-DLLAMA_BUILD_SERVER=OFF -DLLAMA_BUILD_APP=OFF -DLLAMA_BUILD_UI=OFF \
	-DLLAMA_BUILD_COMMON=OFF -DLLAMA_CURL=OFF \
	-DGGML_BACKEND_DL=OFF -DGGML_OPENMP=OFF -DGGML_CPU_REPACK=ON

## llama-lib: Build static llama.cpp/ggml libraries for the host CPU (dev/test)
llama-lib:
	@$(CMAKE) -S $(LLAMA_DIR) -B $(LLAMA_BUILD_DIR) $(LLAMA_CMAKE_COMMON) -DGGML_NATIVE=ON
	@$(CMAKE) --build $(LLAMA_BUILD_DIR) -j $(LLAMA_BUILD_JOBS) --target llama
	@mkdir -p $(LLAMA_BUILD_DIR)/lib
	@find $(LLAMA_BUILD_DIR) -name '*.a' -exec cp -f {} $(LLAMA_BUILD_DIR)/lib/ \;
	@echo "Static libs in $(LLAMA_BUILD_DIR)/lib:"
	@ls $(LLAMA_BUILD_DIR)/lib/*.a

## llama-lib-x86-64: PRIMARY deploy target (ADR-012). Pinned CPU baseline for
## the real Oracle box (VM.Standard.E2.1.Micro -- AMD EPYC 7551 "Naples",
## Zen1: AVX2+FMA+F16C, no AVX-512/AVX-VNNI). GGML_NATIVE=OFF on purpose: the
## build host (GitHub Actions runner, or a dev machine) may have a newer CPU
## with AVX-512, which would SIGILL on the Oracle box if auto-detected.
## Always a native compile (build host and target are both amd64) -- no
## cross-toolchain, no QEMU, ever.
llama-lib-x86-64:
	@$(CMAKE) -S $(LLAMA_DIR) -B $(LLAMA_BUILD_DIR_X86_64) $(LLAMA_CMAKE_COMMON) -DGGML_NATIVE=OFF \
		-DGGML_AVX=ON -DGGML_AVX2=ON -DGGML_FMA=ON -DGGML_F16C=ON \
		-DGGML_AVX_VNNI=OFF -DGGML_AVX512=OFF
	@$(CMAKE) --build $(LLAMA_BUILD_DIR_X86_64) -j $(LLAMA_BUILD_JOBS) --target llama
	@mkdir -p $(LLAMA_BUILD_DIR_X86_64)/lib
	@find $(LLAMA_BUILD_DIR_X86_64) -name '*.a' -exec cp -f {} $(LLAMA_BUILD_DIR_X86_64)/lib/ \;
	# engine_cgo.go's #cgo LDFLAGS hardcodes llama.cpp/build-native/lib (one
	# path for cgo, regardless of which pinned variant was built) -- mirror
	# the pinned archives there so build-native-x86-64 links against these.
	@mkdir -p $(LLAMA_BUILD_DIR)/lib
	@cp -f $(LLAMA_BUILD_DIR_X86_64)/lib/*.a $(LLAMA_BUILD_DIR)/lib/

## llama-lib-arm64: SECONDARY/future target (superseded as the primary path
## by ADR-012 -- the real Oracle box turned out to be x86_64, not ARM64).
## Kept for a possible future ARM64 deploy (Raspberry Pi, or if an A1 shape
## becomes available). True cross-compile for linux/arm64 (Ampere/Neoverse-N1
## class -- dotprod yes, i8mm NO), running natively on an amd64 build host via
## the aarch64-linux-gnu toolchain -- no QEMU emulation (ADR-011). Needs
## `crossbuild-essential-arm64` (Debian/Ubuntu) or equivalent
## (aarch64-linux-gnu-gcc/g++ on PATH).
ARM64_CC?=aarch64-linux-gnu-gcc
ARM64_CXX?=aarch64-linux-gnu-g++
llama-lib-arm64:
	@$(CMAKE) -S $(LLAMA_DIR) -B $(LLAMA_BUILD_DIR_ARM64) $(LLAMA_CMAKE_COMMON) -DGGML_NATIVE=OFF \
		-DGGML_CPU_ARM_ARCH=armv8.2-a+dotprod+fp16 \
		-DCMAKE_SYSTEM_NAME=Linux -DCMAKE_SYSTEM_PROCESSOR=aarch64 \
		-DCMAKE_C_COMPILER=$(ARM64_CC) -DCMAKE_CXX_COMPILER=$(ARM64_CXX)
	@$(CMAKE) --build $(LLAMA_BUILD_DIR_ARM64) -j $(LLAMA_BUILD_JOBS) --target llama
	@mkdir -p $(LLAMA_BUILD_DIR_ARM64)/lib
	@find $(LLAMA_BUILD_DIR_ARM64) -name '*.a' -exec cp -f {} $(LLAMA_BUILD_DIR_ARM64)/lib/ \;
	# engine_cgo.go's #cgo LDFLAGS hardcodes llama.cpp/build-native/lib (one
	# path for cgo, regardless of arch) -- mirror the arm64 archives there so
	# build-native-arm64 links against these and not a stale host-arch build.
	@mkdir -p $(LLAMA_BUILD_DIR)/lib
	@cp -f $(LLAMA_BUILD_DIR_ARM64)/lib/*.a $(LLAMA_BUILD_DIR)/lib/

## build-native: Build the binary with in-process inference (host CPU, cgo)
build-native: generate llama-lib
	CGO_ENABLED=1 $(GO) build $(GOFLAGS),nativellm -ldflags "$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-native$(EXT) ./$(CMD_DIR)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)-native$(EXT)"

## build-native-x86-64: PRIMARY deploy binary (ADR-012) -- pinned AVX2
## baseline, always a native compile (no cross-toolchain, no QEMU).
build-native-x86-64: generate llama-lib-x86-64
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS),nativellm -ldflags "$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-native-linux-amd64 ./$(CMD_DIR)

## nativebench-x86-64: Build the tok/s + RSS benchmark binary (cmd/nativebench)
## against whichever pinned libs are already at llama.cpp/build-native/lib --
## deliberately does NOT depend on llama-lib-x86-64 so it doesn't reconfigure
## the C++ build a second time when run right after build-native-x86-64 in
## the same Dockerfile stage. No model download, no execution -- just the
## binary, shipped in the image so S39 can be (re)measured on the real box.
nativebench-x86-64: generate
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS),nativellm -ldflags "$(LDFLAGS)" \
		-o $(BUILD_DIR)/nativebench-linux-amd64 ./cmd/nativebench

## localllm-integration-test-x86-64: Build (but don't run) the opt-in cgo
## integration test binary for pkg/providers/localllm -- includes
## TestCgoEngine_CoreParking_Integration (B2), which needs the real GGUF and
## the full cgo toolchain, neither available on a 1GB deploy target. Same
## deal as nativebench-x86-64: shipped self-contained (static llama.a, no
## runtime deps beyond glibc) so it can be copied to any host with the real
## model and run standalone with `-test.run`, `KUROMATSU_INTEGRATION_TESTS=1`
## and `KUROMATSU_TEST_MODEL=<path>` -- no Go toolchain needed there.
localllm-integration-test-x86-64: generate
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 $(GO) test -c $(GOFLAGS),nativellm -ldflags "$(LDFLAGS)" \
		-o $(BUILD_DIR)/localllm-integration-test-linux-amd64 ./pkg/providers/localllm/

## build-native-arm64: SECONDARY/future target, see llama-lib-arm64 above.
## True cross-compile of the full binary for linux/arm64, running natively on
## the amd64 build host (ADR-011) -- no QEMU. Same cross-toolchain as
## llama-lib-arm64; cgo cross-compiles fine once CC/CXX point at a valid
## cross-compiler.
build-native-arm64: generate llama-lib-arm64
	CC=$(ARM64_CC) CXX=$(ARM64_CXX) CGO_ENABLED=1 GOOS=linux GOARCH=arm64 \
		$(GO) build $(GOFLAGS),nativellm -ldflags "$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-native-linux-arm64 ./$(CMD_DIR)

## run-native: Build the native binary and download the model if needed
run-native: build-native model-download
	@$(BUILD_DIR)/$(BINARY_NAME)-native$(EXT) status

## bench-native: Build the native binary and run the tok/s + RSS micro-benchmark
bench-native: build-native model-download
	CGO_ENABLED=1 $(GO) build $(GOFLAGS),nativellm -ldflags "$(LDFLAGS)" \
		-o $(BUILD_DIR)/nativebench$(EXT) ./cmd/nativebench
	@$(BUILD_DIR)/nativebench$(EXT) -model models/Bonsai-1.7B-Q1_0.gguf

## build-all: Build the kuromatsu core binary for all Makefile-managed platforms
build-all: generate
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./$(CMD_DIR)
	GOOS=linux GOARCH=arm GOARM=7 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm ./$(CMD_DIR)
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./$(CMD_DIR)
	@$(PTY_PATCH_LOONG64)
	GOOS=linux GOARCH=loong64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-loong64 ./$(CMD_DIR)
	GOOS=linux GOARCH=riscv64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-riscv64 ./$(CMD_DIR)
	GOOS=linux GOARCH=mipsle GOMIPS=softfloat $(GO) build $(GOFLAGS_NO_GOOLM) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-mipsle ./$(CMD_DIR)
	$(call PATCH_MIPS_FLAGS,$(BUILD_DIR)/$(BINARY_NAME)-linux-mipsle)
	GOOS=linux GOARCH=arm GOARM=7 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-armv7 ./$(CMD_DIR)
	GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./$(CMD_DIR)
	GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./$(CMD_DIR)
	GOOS=netbsd GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-netbsd-amd64 ./$(CMD_DIR)
	GOOS=netbsd GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-netbsd-arm64 ./$(CMD_DIR)
	@echo "Core builds complete"

## install: Install kuromatsu to system and copy builtin skills
install: build
	@echo "Installing $(BINARY_NAME)..."
	@mkdir -p $(INSTALL_BIN_DIR)
	# Copy binary with temporary suffix to ensure atomic update
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_BIN_DIR)/$(BINARY_NAME)$(INSTALL_TMP_SUFFIX)
	@chmod +x $(INSTALL_BIN_DIR)/$(BINARY_NAME)$(INSTALL_TMP_SUFFIX)
	@mv -f $(INSTALL_BIN_DIR)/$(BINARY_NAME)$(INSTALL_TMP_SUFFIX) $(INSTALL_BIN_DIR)/$(BINARY_NAME)
	@echo "Installed binary to $(INSTALL_BIN_DIR)/$(BINARY_NAME)"
	@echo "Installation complete!"

## uninstall: Remove kuromatsu from system
uninstall:
	@echo "Uninstalling $(BINARY_NAME)..."
	@rm -f $(INSTALL_BIN_DIR)/$(BINARY_NAME)
	@echo "Removed binary from $(INSTALL_BIN_DIR)/$(BINARY_NAME)"
	@echo "Note: Only the executable file has been deleted."
	@echo "If you need to delete all configurations (config.json, workspace, etc.), run 'make uninstall-all'"

## uninstall-all: Remove kuromatsu and all data
uninstall-all:
	@echo "Removing workspace and skills..."
	@rm -rf $(PICOCLAW_HOME)
	@echo "Removed workspace: $(PICOCLAW_HOME)"
	@echo "Complete uninstallation done!"

## clean: Remove build artifacts
clean:
	@echo "Cleaning build artifacts..."
ifeq ($(OS),Windows_NT)
	@$(POWERSHELL) "if (Test-Path -LiteralPath '$(BUILD_DIR)') { Remove-Item -LiteralPath '$(BUILD_DIR)' -Recurse -Force }"
else
	@rm -rf $(BUILD_DIR)
endif
	@echo "Clean complete"

## vet: Run go vet for static analysis
vet: generate
	@$(GO) vet $(GOFLAGS) ./...

## test: Test Go code
test: generate
	@$(GO) test $(GOFLAGS) ./...

## integration-test: Run Docker-backed integration test suites
integration-test:
	@bash ./scripts/run-integration-tests.sh

## fmt: Format Go code
fmt:
	@$(GOLANGCI_LINT) fmt

## lint: Run linters
lint:
	@$(GOLANGCI_LINT) run --build-tags $(GO_BUILD_TAGS)

## fix: Fix linting issues
fix:
	@$(GOLANGCI_LINT) run --fix --build-tags $(GO_BUILD_TAGS)

## deps: Download dependencies
deps:
	@$(GO) mod download
	@$(GO) mod verify

## model-download: Download the Bonsai GGUF into ./models with SHA256 verification
.PHONY: model-download
model-download:
	@bash ./scripts/download-model.sh models/Bonsai-1.7B-Q1_0.gguf

## update-deps: Update dependencies
update-deps:
	@$(GO) get -u ./...
	@$(GO) mod tidy

## check: Run deps, fmt, vet, and tests
check: deps fmt vet test

## run: Build and run kuromatsu
run: build
	@$(BUILD_DIR)/$(BINARY_NAME) $(ARGS)

## docker-build: Build Docker image (minimal Alpine-based)
docker-build:
	@echo "Building minimal Docker image (Alpine-based)..."
	docker compose -f docker/docker-compose.yml build kuromatsu-agent kuromatsu-gateway

## docker-build-full: Build Docker image with full MCP support (Node.js 24)
docker-build-full:
	@echo "Building full-featured Docker image (Node.js 24)..."
	docker compose -f docker/docker-compose.full.yml build kuromatsu-agent kuromatsu-gateway

## docker-test: Test MCP tools in Docker container
docker-test:
	@echo "Testing MCP tools in Docker..."
	@chmod +x scripts/test-docker-mcp.sh
	@./scripts/test-docker-mcp.sh

## docker-run: Run kuromatsu gateway in Docker (Alpine-based)
docker-run:
	docker compose -f docker/docker-compose.yml --profile gateway up

## docker-run-full: Run kuromatsu gateway in Docker (full-featured)
docker-run-full:
	docker compose -f docker/docker-compose.full.yml --profile gateway up

## docker-run-agent: Run kuromatsu agent in Docker (interactive, Alpine-based)
docker-run-agent:
	docker compose -f docker/docker-compose.yml run --rm kuromatsu-agent

## docker-run-agent-full: Run kuromatsu agent in Docker (interactive, full-featured)
docker-run-agent-full:
	docker compose -f docker/docker-compose.full.yml run --rm kuromatsu-agent

## docker-clean: Clean Docker images and volumes
docker-clean:
	docker compose -f docker/docker-compose.yml down -v
	docker compose -f docker/docker-compose.full.yml down -v
	docker rmi kuromatsu:latest kuromatsu:full 2>/dev/null || true

## docker-build-native: Build the native (nativellm) amd64 image on this host
## -- FALLBACK path, needs real free RAM (see ADR-012); the primary path is
## .github/workflows/build-native-image.yml (GitHub's amd64 runner, 16GB RAM,
## no ceiling). Build host and target are the same architecture (amd64), so
## this is always a plain native compile -- no cross-toolchain, no QEMU.
docker-build-native:
	docker buildx build -f docker/Dockerfile.native \
		-t kuromatsu:native-amd64 --load .

## docker-save-native: Package the built native image as a portable .tar.gz for
## `scp` to the Oracle box -- the deploy target never builds anything, it only
## `docker load`s this file (ADR-012, S32).
docker-save-native: docker-build-native
	@mkdir -p $(BUILD_DIR)
	docker save kuromatsu:native-amd64 | gzip > $(BUILD_DIR)/kuromatsu-native-amd64.tar.gz
	@echo "Saved: $(BUILD_DIR)/kuromatsu-native-amd64.tar.gz"
	@echo "Deploy: scp it to the server, then 'gunzip -c kuromatsu-native-amd64.tar.gz | docker load'"


## mem: Build membench, download LOCOMO data (if needed), run benchmark, and show results
mem:
	@echo "Building membench..."
	@mkdir -p $(BUILD_DIR)
	@$(GO) build -o $(BUILD_DIR)/membench ./cmd/membench
	@echo "Build complete: $(BUILD_DIR)/membench"
	@if [ ! -f $(BUILD_DIR)/memdata/locomo10.json ]; then \
		echo "Downloading LOCOMO dataset..."; \
		mkdir -p $(BUILD_DIR)/memdata; \
		curl -sfL "https://raw.githubusercontent.com/snap-research/locomo/main/data/locomo10.json" \
			-o $(BUILD_DIR)/memdata/locomo10.json && [ -s $(BUILD_DIR)/memdata/locomo10.json ] || { echo "Error: LOCOMO download failed"; exit 1; }; \
		echo "Download complete"; \
	else \
		echo "LOCOMO dataset already exists, skipping download"; \
	fi
	@echo "Running benchmark..."
	@rm -rf $(BUILD_DIR)/memout
	@$(BUILD_DIR)/membench run --data $(BUILD_DIR)/memdata --out $(BUILD_DIR)/memout --budget 4000

## help: Show this help message
help:
	@echo "kuromatsu Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sort | awk -F': ' '{printf "  %-16s %s\n", substr($$1, 4), $$2}'
	@echo ""
	@echo "Examples:"
	@echo "  make build              # Build for current platform"
	@echo "  make install            # Install to ~/.local/bin"
	@echo "  make uninstall          # Remove from /usr/local/bin"
	@echo "  make install-skills     # Install skills to workspace"
	@echo "  make docker-build       # Build minimal Docker image"
	@echo "  make docker-test        # Test MCP tools in Docker"
	@echo ""
	@echo "Environment Variables:"
	@echo "  INSTALL_PREFIX          # Installation prefix (default: ~/.local)"
	@echo "  WORKSPACE_DIR           # Workspace directory (default: ~/.picoclaw/workspace)"
	@echo "  VERSION                 # Version string (default: git describe)"
	@echo ""
	@echo "Current Configuration:"
	@echo "  Platform: $(PLATFORM)/$(ARCH)"
	@echo "  Binary: $(BINARY_PATH)"
	@echo "  Install Prefix: $(INSTALL_PREFIX)"
	@echo "  Workspace: $(WORKSPACE_DIR)"
