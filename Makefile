# Makefile for Bingo microservices

# Directories
GAME_CTRL_DIR = cmd/ctlsvc
CALLER_SVC_DIR = cmd/callersvc
CLAIM_SVC_DIR = cmd/claimsvc

# Binaries
gamecontroller_bin = bin/ctlsvc
callersvc_bin    = bin/callersvc
claimsvc_bin     = bin/claimsvc

# Default: build all services
all: build

# Build all services
build: gamecontroller callersvc claimsvc

# Build individual services
gamecontroller:
	clear
	@echo "Building gamecontroller..."
	@go build -o $(gamecontroller_bin) ./$(GAME_CTRL_DIR)

callersvc:
	clear
	@echo "Building callersvc..."
	@go build -o $(callersvc_bin) ./$(CALLER_SVC_DIR)

claimsvc:
	clear
	@echo "Building claimsvc..."
	@go build -o $(claimsvc_bin) ./$(CLAIM_SVC_DIR)

# Run individual services
run-gamecontroller:
	clear
	@echo "Running gamecontroller..."
	@$(gamecontroller_bin)

run-callersvc:
	clear
	@echo "Running callersvc..."
	@$(callersvc_bin)

run-claimsvc:
	clear
	@echo "Running claimsvc..."
	@$(claimsvc_bin)

# Run all services (in separate terminals or background)
run: run-gamecontroller run-callersvc run-claimsvc

# Clean built binaries
clean:
	clear
	@echo "Cleaning binaries..."
	@rm -rf bin/*

# Tidy go modules
tidy:
	clear
	@echo "Tidying modules..."
	@go mod tidy
