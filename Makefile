.PHONY: build run test test-race test-security-gates vet static full fmt tidy clean bench snapshot snapshot-check

override export GOTOOLCHAIN := go1.27.3

BINARY        := artemis
CMD           := ./cmd/artemis
SNAPSHOT_TOOL := ./cmd/artemis-snapshot
SNAPSHOT_BIN  := js/snapshot.bin
PKGS          := $(shell GOTOOLCHAIN=$(GOTOOLCHAIN) go list ./... 2>/dev/null | grep -v '/research/')
SECURITY_GATE_PKGS := ./network ./bridge ./engine ./download ./process

# snapshot regenerates the V8 startup snapshot and its provenance manifest.
# Run after touching any snapshot-eligible JS bootstrap source under js/
# (TASK-2789). The result is embedded into the artemis binary via go:embed
# and must be checked in.
snapshot:
	go run $(SNAPSHOT_TOOL)
	@ls -lh $(SNAPSHOT_BIN)

snapshot-check:
	go run $(SNAPSHOT_TOOL) --check

build:
	go build -o $(BINARY) $(CMD)

run:
	go run $(CMD)

# test is the default check: tests without vet. vet, static, test-race and
# full (all of them) are separate manual checks.
test:
	go test -vet=off $(PKGS)

test-race:
	go test -race -vet=off $(PKGS)

test-security-gates:
	go test -race -count=1 -timeout=300s -run='^TestSecurityGate' $(SECURITY_GATE_PKGS)
	go test -run='^$$' -fuzz='^FuzzSecurityGatePolicyURL$$' -fuzztime=10s ./network
	go test -run='^$$' -fuzz='^FuzzSecurityGatePolicyValidation$$' -fuzztime=10s ./network

bench:
	go test -bench=. -benchmem -run='^$$' $(PKGS)

vet:
	go vet $(PKGS)

static:
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 $(PKGS)

full: vet static test test-race

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)
	rm -rf bin dist
	go clean -cache -testcache
