# Makefile for Bingo microservices

# Binary names
BINARY_SOCKETSVC  = socketsvc
BINARY_GAMESVC   = gamesvc
BINARY_CTLSVC    = ctlsvc
BINARY_CALLERSVC = callersvc
BINARY_CLAIMSVC  = claimsvc
BINARY_PAYSVC    = paysvc

# Default: build all services
all: build

# Build all services for Linux/amd64
build:
	@echo "Building linux binaries..."
	GOARCH=amd64 GOOS=linux go build -o ./bin/$(BINARY_SOCKETSVC)  ./cmd/socketsvc/main.go
	GOARCH=amd64 GOOS=linux go build -o ./bin/$(BINARY_GAMESVC)   ./cmd/gamesvc/main.go
	GOARCH=amd64 GOOS=linux go build -o ./bin/$(BINARY_CTLSVC)    ./cmd/ctlsvc/main.go
	GOARCH=amd64 GOOS=linux go build -o ./bin/$(BINARY_CALLERSVC) ./cmd/callersvc/main.go
	GOARCH=amd64 GOOS=linux go build -o ./bin/$(BINARY_CLAIMSVC)  ./cmd/claimsvc/main.go
	GOARCH=amd64 GOOS=linux go build -o ./bin/$(BINARY_PAYSVC)    ./cmd/paysvc/main.go
	@echo "Build complete."

# Run individual services
socketsvc: build
	@echo "Running $(BINARY_SOCKETSVC)..."
	@./bin/$(BINARY_SOCKETSVC)

gamesvc: build
	@echo "Running $(BINARY_GAMESVC)..."
	@./bin/$(BINARY_GAMESVC)

ctlsvc: build
	@echo "Running $(BINARY_CTLSVC)..."
	@./bin/$(BINARY_CTLSVC)

callersvc: build
	@echo "Running $(BINARY_CALLERSVC)..."
	@./bin/$(BINARY_CALLERSVC)

claimsvc: build
	@echo "Running $(BINARY_CLAIMSVC)..."
	@./bin/$(BINARY_CLAIMSVC)

paysvc: build
	@echo "Running $(BINARY_PAYSVC)..."
	@./bin/$(BINARY_PAYSVC)

# Directory to hold pid files
PIDDIR := .pids

run-all: build
	@mkdir -p $(PIDDIR)
	@echo "Starting all services..."
	@for svc in \
		$(BINARY_SOCKETSVC) \
		$(BINARY_GAMESVC) \
		$(BINARY_CTLSVC) \
		$(BINARY_CALLERSVC) \
		$(BINARY_CLAIMSVC) \
		$(BINARY_PAYSVC); do \
	  ./bin/$$svc & \
	  echo $$! > $(PIDDIR)/$$svc.pid; \
	done
	@echo "All services started. PIDs in $(PIDDIR)/*.pid"

stop-all:
	@echo "Stopping all services..."
	@for pidfile in $(PIDDIR)/*.pid; do \
	  [ -f $$pidfile ] && kill $$(cat $$pidfile) 2>/dev/null; \
	done
	@rm -rf $(PIDDIR)
	@echo "All services stopped."


# Clean built binaries
clean:
	@echo "Cleaning binaries..."
	@go clean
	@rm -rf bin/*

# Tidy go modules
tidy:
	@echo "Tidying modules..."
	@go mod tidy
