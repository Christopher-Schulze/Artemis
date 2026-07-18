.PHONY: build run test test-race test-security-gates vet fmt tidy clean bench snapshot

BINARY        := artemis
CMD           := ./cmd/artemis
SNAPSHOT_TOOL := ./cmd/artemis-snapshot
SNAPSHOT_BIN  := js/snapshot.bin
PKGS          := $(shell go list ./... 2>/dev/null | grep -v '/research/')
SECURITY_GATE_PKGS := ./network ./bridge ./engine ./download ./process

# snapshot regenerates the V8 startup snapshot. Run after touching any
# JS bootstrap source under js/ (TASK 042). The result is embedded into
# the artemis binary via go:embed and must be checked in.
snapshot:
	go run $(SNAPSHOT_TOOL)
	@ls -lh $(SNAPSHOT_BIN)

build:
	go build -o $(BINARY) $(CMD)

run:
	go run $(CMD)

test:
	go test $(PKGS)

test-race:
	go test -race $(PKGS)

test-security-gates:
	go test -race -count=1 -timeout=300s -run='^TestSecurityGate' $(SECURITY_GATE_PKGS)
	go test -run='^$$' -fuzz='^FuzzSecurityGatePolicyURL$$' -fuzztime=10s ./network
	go test -run='^$$' -fuzz='^FuzzSecurityGatePolicyValidation$$' -fuzztime=10s ./network

bench:
	go test -bench=. -benchmem -run='^$$' $(PKGS)

vet:
	go vet $(PKGS)

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)
	rm -rf bin dist
	go clean -cache -testcache
