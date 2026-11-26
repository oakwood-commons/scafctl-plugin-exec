# scafctl-plugin-exec

Exec provider plugin for scafctl -- executes shell commands using an embedded cross-platform POSIX shell interpreter (`mvdan.cc/sh`) with optional external shell support (`bash`, `pwsh`, `cmd`).

## Capabilities

- `from` -- resolve values from shell command output
- `transform` -- transform data using shell pipelines
- `action` -- run build, test, lint, or operational tasks with real-time output streaming by default

## Installation

~~~bash
# Build from source
task build

# Or download from releases
gh release download --repo github.com/oakwood-commons/scafctl-plugin-exec
~~~

## Usage

Register this plugin in your scafctl configuration, then reference the `exec` provider in your solutions.

### Resolver (`from`)

`failOnError` defaults to `true` across all capabilities, so non-zero exit codes fail fast (and advance `resolve.with` fallback chains) instead of returning blank strings:

~~~yaml
spec:
  resolvers:
    git_sha:
      resolve:
        with:
          - provider: exec
            inputs:
              command: git rev-parse --short HEAD
              raw: true
          - provider: static
            inputs:
              value: dev
~~~

### Exit-code inspection (`failOnError: false`)

Set `failOnError: false` without `raw: true` when you want to inspect non-zero exit codes in the structured output map (`stdout`, `stderr`, `exitCode`, `success`, `command`, `shell`):

~~~yaml
spec:
  resolvers:
    check_tool:
      resolve:
        with:
          - provider: exec
            inputs:
              command: which jq
              failOnError: false
~~~

Note: `raw: true` cannot be combined with `failOnError: false` in `from` or `transform` mode because `raw: true` returns only the trimmed `stdout` string and discards `exitCode` from `Output.Data`.

### Workflow action (`action`)

Actions stream `stdout` and `stderr` in real-time by default while still capturing both in action results (`__actions.<name>.results.stdout` and `-o json` / `-o yaml`). Set `passthrough: false` on an action if you want to buffer output instead:

~~~yaml
spec:
  workflow:
    actions:
      test:
        provider: exec
        inputs:
          command: go test -race ./...
          echo: true
~~~

## Inputs

| Input | Type | Default | Description |
|-------|------|---------|-------------|
| `command` | `string` | *(required)* | POSIX shell command to execute |
| `args` | `array` | `[]` | Additional arguments (automatically shell-quoted) |
| `stdin` | `string` | `""` | Standard input passed to the command |
| `workingDir` | `string` | context / CWD | Working directory for command execution |
| `env` | `object` | `{}` | Environment variables merged with parent environment (`NO_COLOR=1` and `TERM=dumb` injected unless overridden) |
| `timeout` | `integer` | `0` | Timeout in seconds (`0` for no timeout) |
| `shell` | `string` | `"auto"` | Shell interpreter: `auto`, `sh`, `bash`, `pwsh`, `cmd` |
| `raw` | `boolean` | `false` | Return trimmed `stdout` string instead of full map (`from`/`transform` only) |
| `passthrough` | `boolean` | `true` (`action`), `false` (`from`/`transform`) | Stream `stdout` and `stderr` in real-time while keeping both captured in results |
| `echo` | `boolean` | `false` | Print the resolved command to `stderr` before executing |
| `failOnError` | `boolean` | `true` | Return an error when the command exits with a non-zero exit code |

## Development

~~~bash
# Run tests
task test

# Run tests with race detector
task test:race

# Run linter
task lint

# Build
task build

# Full CI pipeline
task ci
~~~

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache-2.0 -- see [LICENSE](LICENSE) for details.
