# tt — build & install
#
# The bash `tt` needs no build (edit-and-run). The Go rewrite does, so this
# Makefile is the install path while both exist side by side:
#
#   make install      -> ~/.local/bin/tt-go   (dogfood alongside bash `tt`)
#   make cutover      -> ~/.local/bin/tt      (flip the live tool to Go)
#   make restore-bash -> ~/.local/bin/tt      (point back at the bash tool)
#
# See docs/PLAN.md Phase 4.

BIN_DIR ?= $(HOME)/.local/bin
REPO_DIR := $(shell pwd)

.PHONY: build vet test check install cutover restore-bash clean

build:
	go build -o tt-go .

vet:
	go vet ./...

test:
	go test ./...

check: build vet test

# Install the Go binary alongside bash `tt` as `tt-go` — non-destructive.
install: build
	mkdir -p "$(BIN_DIR)"
	install -m 0755 tt-go "$(BIN_DIR)/tt-go"
	@echo "installed $(BIN_DIR)/tt-go (bash \`tt\` untouched)"

# Flip the live tool to Go. `tt` becomes a real binary, replacing the symlink
# to the bash script. Reversible with `make restore-bash`.
cutover: check
	mkdir -p "$(BIN_DIR)"
	rm -f "$(BIN_DIR)/tt"
	install -m 0755 tt-go "$(BIN_DIR)/tt"
	@echo "cutover: $(BIN_DIR)/tt is now the Go binary"

# Point the live tool back at the bash script.
restore-bash:
	rm -f "$(BIN_DIR)/tt"
	ln -s "$(REPO_DIR)/tt" "$(BIN_DIR)/tt"
	@echo "restored: $(BIN_DIR)/tt -> $(REPO_DIR)/tt (bash)"

clean:
	rm -f tt-go
