This is not an officially supported Google product.

# selene

This is an experiment to enable mutation testing in Go.

The first case will be the reversal of bolean expressions in if conditionals.

The technique used here is to read the source files, parse the AST, replace relevant nodes and write the AST back to a modified source in a temporary folder.

Then we run `go test` using an overlay to replace the original files with the mutated ones.

## Running the experiment

```
$ go build
$ ./selene testdata/cond.go
=== RUN   TestCond
--- FAIL: TestCond (0.00s) - MUTATION CAUGHT
=== RUN   TestFake
--- PASS: TestFake (0.00s) - MUTATION NOT CAUGHT
FAIL
1 out of 2 tests didn't catch any mutations
```

You can also set GOMUTATION as directory for the output of the mutated files and overlay. If not specified selene will use a temporary directory.

```
$ GOMUTATION=./testdata/mutation ./selene testdata/cond.go
```

## Why Selene?

Selene is the [oldest known human mutant](https://en.wikipedia.org/wiki/Selene_(comics)) in Marvel comics. It's also the name of the best protagonist of a vampire movie ever.