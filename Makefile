# tt — build & install
#
# tt is a Go program: `make install` (or `make cutover`) is the install step.
# The bash implementation was retired at v0.16.0; recover it from its tag with
# `make restore-bash` if a regression ever forces a fallback.
#
#   make install      -> ~/.local/bin/tt-go   (side-by-side, for testing)
#   make cutover      -> ~/.local/bin/tt      (the live tool)
#   make restore-bash -> restore the bash tool from its tag
#
# See docs/PLAN.md Phase 4.

BIN_DIR ?= $(HOME)/.local/bin
REPO_DIR := $(shell pwd)
BASH_TAG ?= v0.15.3-bash-final

.PHONY: build vet test check install cutover restore-bash clean

build:
	go build -o tt-go .

vet:
	go vet ./...

test:
	go test ./...

check: build vet test

# Install the Go binary alongside the live tool as `tt-go` — non-destructive.
install: build
	mkdir -p "$(BIN_DIR)"
	install -m 0755 tt-go "$(BIN_DIR)/tt-go"
	@echo "installed $(BIN_DIR)/tt-go"

# Install the Go binary as the live `tt`.
cutover: check
	mkdir -p "$(BIN_DIR)"
	rm -f "$(BIN_DIR)/tt"
	install -m 0755 tt-go "$(BIN_DIR)/tt"
	@echo "cutover: $(BIN_DIR)/tt is now the Go binary"

# Emergency fallback: restore the retired bash tool from its tag and point the
# live `tt` at it. Only for a regression the Go tool cannot cover.
restore-bash:
	git checkout $(BASH_TAG) -- tt
	chmod +x tt
	rm -f "$(BIN_DIR)/tt"
	ln -s "$(REPO_DIR)/tt" "$(BIN_DIR)/tt"
	@echo "restored: $(BIN_DIR)/tt -> $(REPO_DIR)/tt (bash $(BASH_TAG))"
	@echo "NOTE: ./tt is now an untracked checkout of the retired script."

clean:
	rm -f tt-go
