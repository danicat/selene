# Selene - Gemini CLI Development Notes

## TestQuery (tq) Usage Tips

The `tq` tool is invaluable for debugging coverage anomalies and understanding the relationship between specific tests and the code they exercise.

### Basic Workflow
1.  **Initialize the Database**: Run `tq build ./...` (or specify packages like `tq build ./internal/...`) to generate `testquery.db`.
2.  **Verify Schema**: Use `tq query "PRAGMA table_info(test_coverage)"` to understand available columns (file, line, count, test_name, etc.).
3.  **Check Coverage for a Specific Line**:
    ```bash
    tq query "SELECT test_name, count FROM test_coverage WHERE file = 'your_file.go' AND start_line <= 50 AND end_line >= 50"
    ```
4.  **Find All Functions with Coverage**:
    ```bash
    tq query "SELECT DISTINCT file, function_name FROM test_coverage WHERE count > 0"
    ```

### Strategic Tips
*   **Module Root**: If `tq build` fails with "no Go files", ensure you are running it from a directory containing a `go.mod` or specify the package path explicitly.
*   **Honest Metrics**: Use `tq` to identify if a line is truly hit by a test or just appears covered due to package-level execution.
*   **Path Matching**: When querying, check `SELECT DISTINCT file FROM test_coverage` first to see if paths are stored as absolute or relative, as this varies by environment.
