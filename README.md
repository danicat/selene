# selene

Selene is a mutation testing tool for Go. It helps you verify the quality of your test suite by ensuring that your tests fail when the code is modified (mutated).

## How it works

The tool reads the source files, parses the AST, replaces relevant nodes (mutations), and writes the modified source to a temporary folder. Then, it runs `go test` using the `-overlay` flag to replace the original files with the mutated ones during compilation.

## Supported Mutators

- **ReverseIfCond**: Reverses boolean expressions in `if` statements (e.g., `if x > 0` becomes `if !(x > 0)`).
- **SwapArithmetic**: Swaps arithmetic operators (e.g., `+` becomes `-`, `*` becomes `/`).

## Installation

Install directly using `go install`:

```bash
go install github.com/danicat/selene/cmd/selene@latest
```

Or build from source:

```bash
go build -o selene cmd/selene/main.go
```

## Usage

Run mutation testing on specific files:

```bash
./selene run <file1.go> <file2.go> ...
```

### Example

```bash
$ ./selene run testdata/cond.go
Mutation directory: /var/folders/.../T/mutation12345
ReverseIfCond-testdata/cond.go:6: killed
Score: 1/1 (100.00%)
```

### Options

| Flag | Description |
|------|-------------|
| `--mutation-dir` | Directory to store mutations (default: temporary directory). |
| `--mutators` | Comma-separated list of mutators to enable (e.g., `ReverseIfCond,SwapArithmetic`). If empty, all mutators are enabled by default. |

Selene automatically runs `go test` to generate a coverage profile and skips mutations on uncovered code.

```bash
# Run with specific mutators
./selene run --mutators=SwapArithmetic testdata/arithmetic.go
```

## Why Selene?

Selene is the [oldest known human mutant](https://en.wikipedia.org/wiki/Selene_(comics)) in Marvel comics. It's also the name of the best protagonist of a vampire movie ever.
