package mutator

import (
	"fmt"
	"go/ast"
	"go/token"
)

// DestructiveSinks defines package-qualified function names that perform destructive operations.
var DestructiveSinks = map[string]map[string]bool{
	"os": {
		"RemoveAll": true,
		"Remove":    true,
		"Truncate":  true,
		"Chmod":     true,
		"Chown":     true,
		"Exit":      true,
	},
	"exec": {
		"Command":        true,
		"CommandContext": true,
	},
	"syscall": {
		"Unlink": true,
		"Rmdir":  true,
		"Kill":   true,
		"Exec":   true,
	},
}

// IsDestructiveCall checks if an AST CallExpr calls a known destructive sink.
// Returns true and the formatted sink name (e.g., "os.RemoveAll").
func IsDestructiveCall(call *ast.CallExpr) (bool, string) {
	if call == nil {
		return false, ""
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false, ""
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false, ""
	}

	pkgSinks, exists := DestructiveSinks[ident.Name]
	if exists && pkgSinks[sel.Sel.Name] {
		return true, fmt.Sprintf("%s.%s", ident.Name, sel.Sel.Name)
	}
	return false, ""
}

// ContainsDestructiveCall inspects a statement or block to determine if it executes a destructive call.
func ContainsDestructiveCall(n ast.Node) (bool, string) {
	if n == nil {
		return false, ""
	}
	var found bool
	var sinkName string

	ast.Inspect(n, func(node ast.Node) bool {
		if found {
			return false
		}
		if call, ok := node.(*ast.CallExpr); ok {
			if isDest, name := IsDestructiveCall(call); isDest {
				found = true
				sinkName = name
				return false
			}
		}
		return true
	})

	return found, sinkName
}

// BuildDestructiveExclusionSet automatically scans an AST file and identifies all AST nodes
// that are arguments or conditional safety guards to destructive sinks, ensuring zero host damage out-of-the-box.
func BuildDestructiveExclusionSet(file *ast.File, _ *token.FileSet) map[ast.Node]string {
	exclusions := make(map[ast.Node]string)
	if file == nil {
		return exclusions
	}

	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			return true
		}

		switch node := n.(type) {
		case *ast.CallExpr:
			// A. Protect arguments to destructive calls: os.RemoveAll(arg), exec.Command(...)
			if isDest, sink := IsDestructiveCall(node); isDest {
				reason := fmt.Sprintf("argument to %s", sink)
				for _, arg := range node.Args {
					markNodeAndChildren(arg, reason, exclusions)
				}
			}

		case *ast.IfStmt:
			// B. Protect conditional guards wrapping destructive blocks
			if containsDest, sink := ContainsDestructiveCall(node.Body); containsDest {
				reason := fmt.Sprintf("guard for %s in if-block", sink)
				markNodeAndChildren(node.Cond, reason, exclusions)
			}
			if node.Else != nil {
				if containsDest, sink := ContainsDestructiveCall(node.Else); containsDest {
					reason := fmt.Sprintf("guard for %s in else-block", sink)
					markNodeAndChildren(node.Cond, reason, exclusions)
				}
			}
		}

		return true
	})

	return exclusions
}

func markNodeAndChildren(root ast.Node, reason string, set map[ast.Node]string) {
	if root == nil {
		return
	}
	ast.Inspect(root, func(n ast.Node) bool {
		if n != nil {
			set[n] = reason
		}
		return true
	})
}
