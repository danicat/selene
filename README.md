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
go install github.com/danicat/selene@latest
```

Or build from source:

```bash
go build -o selene .
```

## Usage

Run mutation testing on specific files or package patterns:

```bash
# Run on specific files
./selene run <file1.go> <file2.go> ...

# Run on all files in the current package and subpackages
./selene run ./...
```

### Example

```bash
$ ./selene run ./...
Mutation directory: /var/folders/.../T/mutation12345
Running tests to generate coverage profile...
ReverseIfCond-internal/mutator/mutator.go:50:8: killed
SwapArithmetic-internal/mutator/swap_arithmetic.go:19:52: killed
Comparison-internal/mutator/comparison.go:10:15: uncovered

Total mutations: 3
Killed:          2
Survived:        0
Uncovered:       1
Mutation Score:  100.00% (killed/covered)
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
