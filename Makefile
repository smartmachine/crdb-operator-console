# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOGENERATE=$(GOCMD) generate
BINARY_NAME=crdb-operator-console

build_cmd: 
	$(GOBUILD) -o $(BINARY_NAME) -v ./cmd/$(BINARY_NAME)
clean: 
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
run_cmd:
	./$(BINARY_NAME)
generate:
	$(GOGENERATE) ./cmd/$(BINARY_NAME)
run: build_cmd run_cmd
build: build_cmd
