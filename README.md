# selene

This is an experiment to enable mutation testing in Go.

The first case will be the reversal of bolean expressions in if conditionals.

The technique used here is to read the source files, parse the AST, replace relevant nodes and write the AST back to a modified source in a temporary folder.

Then we run `go test` using an overlay to replace the original files with the mutated ones.

## Running the experiment

```
mkdir -p testdata/mutations
go run main.go > testdata/mutations/cond.go
cd testdata
go test --overlay mutations/overlay.json .
```