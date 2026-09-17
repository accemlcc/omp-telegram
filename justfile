set default-script
set script-interpreter := ["bash", "-euo", "pipefail"]

# Build the daemon. This is the default recipe.
build:
    go build -o omp-telegram ./cmd/omp-telegram

# Install only the binary; configuration and data remain user-managed.
install bin_dir=(env_var("HOME") / "tool/omp-telegram"): build
    install -d {{quote(bin_dir)}}
    install -m 755 omp-telegram {{quote(bin_dir / "omp-telegram")}}

# Install the updated binary and restart the existing go-supervisor service.
test: install
    supervisord ctl restart omp-telegram

# Run automated tests, race checks, and vet without starting Telegram polling.
check:
    go test ./...
    go test -race ./...
    go vet ./...
