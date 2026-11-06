// path: internal/pipeline/parser.go
package pipeline

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseError represents an error encountered during YAML parsing or schema validation.
type ParseError struct {
	Line    int
	Column  int
	Field   string
	Message string
}

func (e *ParseError) Error() string {
	if e.Line > 0 && e.Column > 0 {
		return fmt.Sprintf("line %d, col %d: %s", e.Line, e.Column, e.Message)
	}
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s", e.Line, e.Message)
	}
	return e.Message
}

// Parse decodes, strictly validates, and verifies a pipeline definition from an io.Reader.
// Unknown keys and schema violations are rejected with descriptive line numbers.
func Parse(r io.Reader) (*Pipeline, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading pipeline definition: %w", err)
	}
	return ParseBytes(data)
}

// ParseBytes decodes and validates a pipeline from raw YAML bytes.
func ParseBytes(data []byte) (*Pipeline, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, &ParseError{Line: 1, Message: "pipeline definition is empty"}
	}

	// 1. AST Decode into yaml.Node to retain line and column positions.
	var rootNode yaml.Node
	if err := yaml.Unmarshal(data, &rootNode); err != nil {
		return nil, formatYamlError(err)
	}

	// 2. Strict decoding into Pipeline struct rejecting unknown fields.
	var p Pipeline
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return nil, formatYamlError(err)
	}

	// 3. Semantic schema validation with line resolution.
	if err := Validate(&p, &rootNode); err != nil {
		return nil, err
	}

	return &p, nil
}

// formatYamlError normalizes errors from yaml.v3 into clear line-numbered ParseErrors.
func formatYamlError(err error) error {
	if typeErr, ok := err.(*yaml.TypeError); ok && len(typeErr.Errors) > 0 {
		// Example: "line 4: field invalid_key not found in type pipeline.Pipeline"
		first := typeErr.Errors[0]
		var line int
		var msg string
		if n, _ := fmt.Sscanf(first, "line %d: %s", &line, &msg); n >= 1 {
			msg = first[strings.Index(first, ": ")+2:]
			return &ParseError{Line: line, Message: msg}
		}
		return &ParseError{Message: first}
	}
	return &ParseError{Message: err.Error()}
}

