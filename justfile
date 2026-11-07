version    := `git describe --tags --always --dirty 2>/dev/null || echo "dev"`
bin        := "copilot-sessions"
go_version := `awk '/^go /{split($2, v, "."); print v[1] "." v[2]}' go.mod`

# List recipes
@_:
    just --list

# Build the binary (no CGO)
[group('build')]
build:
    CGO_ENABLED=0 go build -ldflags "-X main.Version={{version}}" -o {{bin}} ./cmd/copilot-sessions

# Run tests
[group('qa')]
test:
    go test ./...

# Run tests on Linux in Docker, so the /proc-dependent tests run from macOS
[group('qa')]
test-linux:
    docker run --rm -v "$PWD":/src -w /src \
      -v go-mod:/go/pkg/mod -v go-build:/root/.cache/go-build \
      golang:{{go_version}} go test -count=1 ./...

# Static checks: formatting + go vet
[group('qa')]
lint:
    @unformatted="$(gofmt -l .)"; \
      if [ -n "$unformatted" ]; then echo "gofmt drift:"; echo "$unformatted"; exit 1; fi
    go vet ./...

# Check formatting only
[group('qa')]
fmt-check:
    @unformatted="$(gofmt -l .)"; \
      if [ -n "$unformatted" ]; then echo "gofmt drift:"; echo "$unformatted"; exit 1; fi

# Format in place
[group('qa')]
fmt:
    gofmt -w .

# Print the version the binary reports
[group('release')]
show-version: build
    ./{{bin}} --version

# Install the binary to ~/.local/bin (replaces the Python script)
[group('lifecycle')]
install: build
    mkdir -p ~/.local/bin
    install -m 0755 {{bin}} ~/.local/bin/{{bin}}

# Regenerate the zsh completion in ~/.zsh/completions
[group('lifecycle')]
completion: build
    mkdir -p ~/.zsh/completions
    ./{{bin}} completion zsh > ~/.zsh/completions/_{{bin}}

# Remove build output
[group('lifecycle')]
clean:
    rm -f {{bin}}

# vim: set ft=make et ai:
