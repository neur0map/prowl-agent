package cli

import (
	"encoding/json"

	toon "github.com/toon-format/toon-go"
)

// outputFormat selects how query results are serialized for the CLI.
type outputFormat int

const (
	// formatTOON is the default: Token-Oriented Object Notation. Models read it
	// ~40% cheaper than JSON (and slightly more accurately) because uniform
	// arrays of objects collapse to a single header plus CSV-style rows, which
	// is exactly the shape of prowl's results (symbols, edges, chunks).
	formatTOON outputFormat = iota
	// formatJSON emits compact JSON, matching what the MCP server returns.
	formatJSON
)

// formatValue serializes v in the requested format.
//
// Both formats go through encoding/json first, so the output honors the json
// struct tags already on every result type (snake_case names, omitempty, and
// json:"-" exclusions). TOON then re-encodes that generic JSON value, so the
// TOON and JSON outputs describe the identical data; TOON is just token-leaner.
func formatValue(v any, format outputFormat) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	if format == formatJSON {
		return string(data), nil
	}
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		return "", err
	}
	return toon.MarshalString(generic)
}
