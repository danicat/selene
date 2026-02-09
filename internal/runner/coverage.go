package runner

import (
	"bufio"
	"path/filepath"

	"os"
	"strconv"
	"strings"
)

// Coverage represents code coverage data.
type Coverage struct {
	Blocks map[string][]Block
}

// Block represents a covered code block.
type Block struct {
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	Count     int
}

// LoadCoverage loads coverage data from a profile file.
func LoadCoverage(filename string) (*Coverage, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	cov := &Coverage{Blocks: make(map[string][]Block)}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "mode:") {
			continue
		}
		// Format: name.go:line.col,line.col numStmt count
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}
		file := parts[0]
		rest := parts[1]

		fields := strings.Fields(rest)
		if len(fields) != 3 {
			continue
		}

		rangeParts := strings.Split(fields[0], ",")
		if len(rangeParts) != 2 {
			continue
		}

		startParts := strings.Split(rangeParts[0], ".")
		endParts := strings.Split(rangeParts[1], ".")

		startLine, _ := strconv.Atoi(startParts[0])
		startCol, _ := strconv.Atoi(startParts[1])
		endLine, _ := strconv.Atoi(endParts[0])
		endCol, _ := strconv.Atoi(endParts[1])
		count, _ := strconv.Atoi(fields[2])

		if count > 0 {
			cov.Blocks[file] = append(cov.Blocks[file], Block{
				StartLine: startLine,
				StartCol:  startCol,
				EndLine:   endLine,
				EndCol:    endCol,
				Count:     count,
			})
		}
	}

	return cov, scanner.Err()
}

// IsCovered checks if a position is covered.
// Note: filename in coverage profile might be relative or absolute depending on how test was run.
// We assume simple matching for now.
func (c *Coverage) IsCovered(filename string, line int) bool {
	// Try to match filename suffix
	for covFile, blocks := range c.Blocks {
		match := false
		if strings.HasSuffix(filename, covFile) {
			match = true
		} else if strings.HasSuffix(covFile, filepath.Base(filename)) {
			// If coverage file ends with our filename (e.g. pkg/file.go ends with file.go)
			// This is useful when comparing absolute paths to relative ones in profile.
			match = true
		} else {

			// If covFile has a module prefix, try to match after the last / of the module name
			// This is still a bit simplified but better than before.
			// Actually, the most robust way is to see if the filename ends with the covFile
			// after we possibly strip some components.
			// Let's try matching the components from right to left.
			fParts := strings.Split(filename, "/")
			cParts := strings.Split(covFile, "/")

			if len(fParts) > 0 && len(cParts) > 0 {
				i := len(fParts) - 1
				j := len(cParts) - 1
				for i >= 0 && j >= 0 && fParts[i] == cParts[j] {
					i--
					j--
				}
				// If we matched at least the filename and the immediate parent dir
				if (len(cParts)-1-j) >= 2 || (len(cParts) == 1 && j == -1) {
					match = true
				}
			}
		}

		if match {
			for _, b := range blocks {
				if line >= b.StartLine && line <= b.EndLine {
					return true
				}
			}
		}
	}
	return false
}
