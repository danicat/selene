package runner

import (
	"bufio"

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
	defer f.Close()

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
		if strings.HasSuffix(filename, covFile) || strings.HasSuffix(covFile, filename) {
			for _, b := range blocks {
				if line >= b.StartLine && line <= b.EndLine {
					return true
				}
			}
		}
	}
	return false
}
