package golden

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "update golden files")

// Regex patterns for standard UUIDs (v4) and ISO8601/RFC3339 timestamps.
var uuidRegex = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)
var timestampRegex = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})`)

// AssertJSON normalizes the actual JSON, pretty-prints it, and compares it to a golden file.
// If the -update flag is passed, it writes the normalized JSON to the golden file instead.
func AssertJSON(t *testing.T, actualJSON []byte, goldenFilename string) {
	t.Helper()

	// Normalize UUIDs
	normalized := uuidRegex.ReplaceAll(actualJSON, []byte("<UUID>"))
	// Normalize Timestamps
	normalized = timestampRegex.ReplaceAll(normalized, []byte("<TIMESTAMP>"))

	// Format the JSON to be pretty-printed (indented)
	var prettyJSON bytes.Buffer
	err := json.Indent(&prettyJSON, normalized, "", "  ")
	require.NoError(t, err, "Failed to pretty-print JSON")

	normalizedOutput := prettyJSON.Bytes()

	if *update {
		err = os.WriteFile(goldenFilename, normalizedOutput, 0644)
		require.NoError(t, err, "Failed to write golden file %s", goldenFilename)
		return
	}

	expectedOutput, err := os.ReadFile(goldenFilename)
	require.NoError(t, err, "Failed to read golden file %s", goldenFilename)

	// Compare the normalized actual JSON to the file's contents using bytes.Equal
	if !bytes.Equal(expectedOutput, normalizedOutput) {
		// Use testify's assert.Equal on strings to print a clear diff and fail the test
		assert.Equal(t, string(expectedOutput), string(normalizedOutput), "JSON does not match golden file %s", goldenFilename)
	}
}
