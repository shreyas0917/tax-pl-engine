// Package jsonreport serialises a pipeline report deterministically (stable key order,
// stable number formatting) so that identical inputs produce byte-identical output.
package jsonreport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/shreyas0917/taxpl/internal/pipeline"
)

// Marshal renders the report as indented JSON. encoding/json writes struct fields in
// declaration order and map keys sorted, and shopspring decimals marshal as strings, so the
// output is deterministic.
func Marshal(rep *pipeline.Report) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rep); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Checksum is the SHA-256 of the marshalled report, recorded with every generated report.
func Checksum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
