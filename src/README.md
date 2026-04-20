# mdm

Go source for the `mdm` CLI.

## Run without building

```bash
go run . [command] [args]

go run . --help
go run . skills list
go run . skills add vercel-labs/agent-skills
```

## Build a binary

```bash
# Build into the current directory
make build
# or directly:
go build -o mdm .

# Run it
./mdm --help
```

## Run tests

```bash
make test
# or directly:
go test ./...
```

## Install to $GOPATH/bin (makes `mdm` available system-wide)

```bash
make install
# or directly:
go install .
mdm --help
```

## Debug with Delve (CLI)

```bash
# Install once
go install github.com/go-delve/delve/cmd/dlv@latest

# Start a debug session (args after -- are passed to the program)
dlv debug . -- add vercel-labs/agent-skills
```

Common Delve commands inside a session:

| Command | Description |
|---|---|
| `break main.go:42` | Set a breakpoint |
| `continue` | Run until next breakpoint |
| `next` | Step over |
| `step` | Step into |
| `print varName` | Inspect a variable |
| `quit` | Exit |

## Debug with VS Code

Open the **Run and Debug** panel (`Ctrl+Shift+D`), pick a configuration, set breakpoints, press **F5**.
Configurations live in [../.vscode/launch.json](../.vscode/launch.json).
