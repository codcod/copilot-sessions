version    := `git describe --tags --always --dirty 2>/dev/null || echo "dev"`
bin        := "copilot-sessions"
go_version := `awk '/^go /{split($2, v, "."); print v[1] "." v[2]}' go.mod`

# List recipes
@_:
    just --list

# Build the binary (no CGO)
[group('build')]
build:
    CGO_ENABLED=0 go build -ldflags "-X main.version={{version}}" -o {{bin}} ./cmd/copilot-sessions

# Run tests
[group('qa')]
test:
    go test ./...

# Run tests on Linux in Docker, so the Linux (/proc) path is tested from macOS
[group('qa')]
test-linux:
    docker run --rm -v "$PWD":/src -w /src \
      -v go-mod:/go/pkg/mod -v go-build:/root/.cache/go-build \
      golang:{{go_version}} go test -count=1 ./...

# Static checks: formatting + go vet + go fix modernizers
[group('qa')]
lint:
    @unformatted="$(gofmt -l .)"; \
      if [ -n "$unformatted" ]; then echo "gofmt drift:"; echo "$unformatted"; exit 1; fi
    go vet ./...
    go fix -diff ./...

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
    ./{{bin}} version

# Validate .goreleaser.yaml (GITLAB_TOKEN unset so goreleaser cannot pick GitLab)
[group('release')]
dist-check:
    env -u GITLAB_TOKEN -u GITLAB_PERSONAL_ACCESS_TOKEN goreleaser check

# Build an unpublished snapshot into dist/ (GITLAB_TOKEN unset so goreleaser cannot pick GitLab)
[group('release')]
dist-snapshot:
    env -u GITLAB_TOKEN -u GITLAB_PERSONAL_ACCESS_TOKEN goreleaser release --snapshot --clean

# Install the binary to ~/.local/bin (replaces the Python script)
[group('lifecycle')]
install: build
    mkdir -p ~/.local/bin
    install -m 0755 {{bin}} ~/.local/bin/{{bin}}

# Remove build output
[group('lifecycle')]
clean:
    rm -f {{bin}}

# vim: set ft=make et ai:
