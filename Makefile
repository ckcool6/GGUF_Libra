# Shell configuration
SHELL := /bin/bash

# ANSI Color definitions
C_RESET  := \033[0m
C_BOLD   := \033[1m
C_RED    := \033[31m
C_GREEN  := \033[32m
C_CYAN   := \033[36m
C_BLUE   := \033[34m

# Status tags
TAG_STEP := $(C_BOLD)$(C_CYAN)[STEP]$(C_RESET)
TAG_OK   := $(C_BOLD)$(C_GREEN)[OK]$(C_RESET)
TAG_FAIL := $(C_BOLD)$(C_RED)[FAIL]$(C_RESET)
TAG_INFO := $(C_BOLD)$(C_BLUE)[INFO]$(C_RESET)

RELEASE_DIR       := release
EXE_NAME          := gguf-libra
PARSER_SRC_DIR    := parser_py
TARGET_PARSER_DIR := $(CURDIR)/$(RELEASE_DIR)/parser_py

.PHONY: all init frontend backend assets parser clean

.NOTPARALLEL: all

# Default target: build all components
all: init frontend backend assets parser
	@printf "\n$(C_BOLD)================ Build Completed Successfully ================$(C_RESET)\n"
	@printf "Release package structure:\n"
	@printf "$(RELEASE_DIR)/\n"
	@printf "├── $(EXE_NAME)\n"
	@printf "├── data.bin               (empty placeholder)\n"
	@printf "├── system_prompt.yaml\n"
	@printf "├── chain_history.json\n"
	@printf "├── dist/                  (bundled frontend assets)\n"
	@printf "└── parser_py/\n"
	@printf "    └── parser             (standalone Python binary, zero dependencies required)\n\n"

# 0. Clean and initialize release directory
init:
	@printf "$(C_BOLD)================ Starting Build Pipeline (Linux) ================$(C_RESET)\n"
	@rm -rf $(RELEASE_DIR)
	@mkdir -p $(RELEASE_DIR)

# 1. Build frontend distribution
frontend:
	@printf "\n$(TAG_STEP) Building frontend distribution...\n"
	npm install
	npm run build
	@printf "$(TAG_OK) Frontend build complete.\n"

# 2. Compile Go binary into release directory
backend:
	@printf "\n$(TAG_STEP) Compiling Go binary into release directory...\n"
	go mod download
	go build -x -o "$(RELEASE_DIR)/$(EXE_NAME)"
	@printf "$(TAG_OK) Go compilation complete.\n"

# 3. Archive base runtime assets and configuration files
assets:
	@printf "\n$(TAG_INFO) Archiving base runtime assets...\n"
	@cp -r dist $(RELEASE_DIR)/dist
	@printf "$(TAG_INFO) Copied directory: dist/\n"
	@if [ -f "system_prompt.yaml" ]; then \
		cp system_prompt.yaml $(RELEASE_DIR)/; \
		printf "$(TAG_INFO) Copied file: system_prompt.yaml\n"; \
	fi
	@if [ -f "chain_history.json" ]; then \
		cp chain_history.json $(RELEASE_DIR)/; \
		printf "$(TAG_INFO) Copied file: chain_history.json\n"; \
	fi
	@touch $(RELEASE_DIR)/data.bin
	@printf "$(TAG_OK) Created empty data.bin placeholder in release directory\n"

# 4. Compile Python parser into standalone executable (uv + pyinstaller)
parser:
	@printf "\n$(TAG_INFO) Compiling Python parser to standalone executable...\n"
	@mkdir -p $(TARGET_PARSER_DIR)
	cd $(PARSER_SRC_DIR) && uv sync
	cd $(PARSER_SRC_DIR) && uv run pyinstaller --onefile --console --clean --icon=NONE --distpath "$(TARGET_PARSER_DIR)" --name parser main.py
	@rm -rf $(PARSER_SRC_DIR)/build $(PARSER_SRC_DIR)/*.spec
	@printf "$(TAG_OK) Python parser compiled successfully.\n"

# Clean artifacts
clean:
	@printf "$(TAG_STEP) Cleaning build artifacts...\n"
	@rm -rf $(RELEASE_DIR)
	@rm -rf $(PARSER_SRC_DIR)/build $(PARSER_SRC_DIR)/*.spec
	@printf "$(TAG_OK) Clean done.\n"