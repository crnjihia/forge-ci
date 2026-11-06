// path: internal/pipeline/pipeline_test.go
package pipeline

import (
	"strings"
	"testing"
)

func TestParse_CanonicalPipeline(t *testing.T) {
	yamlInput := `
name: build-and-test
env:
  GO_VERSION: "1.22"
steps:
  - id: lint
    image: golangci/golangci-lint:latest
    run: golangci-lint run
  - id: test
    image: golang:1.22
    run: go test ./...
    depends-on: [lint]
    resources:
      cpu: 1.0
      memory: 512m
  - id: build
    image: golang:1.22
    run: go build -o dist/app ./cmd/app
    depends-on: [test]
    artifacts:
      - dist/**
secrets:
  - DEPLOY_TOKEN
`
	p, err := ParseBytes([]byte(yamlInput))
	if err != nil {
		t.Fatalf("expected successful parse, got: %v", err)
	}

	if p.Name != "build-and-test" {
		t.Errorf("expected name 'build-and-test', got %q", p.Name)
	}
	if p.Env["GO_VERSION"] != "1.22" {
		t.Errorf("expected env GO_VERSION='1.22', got %q", p.Env["GO_VERSION"])
	}
	if len(p.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(p.Steps))
	}
	if p.Steps[1].Resources == nil || p.Steps[1].Resources.CPU != 1.0 || p.Steps[1].Resources.Memory != "512m" {
		t.Errorf("expected step[1] resources cpu: 1.0, memory: 512m, got %+v", p.Steps[1].Resources)
	}
	if len(p.Secrets) != 1 || p.Secrets[0] != "DEPLOY_TOKEN" {
		t.Errorf("expected secret DEPLOY_TOKEN, got %+v", p.Secrets)
	}

	// Verify StepByID and StepIDs helper methods
	step, ok := p.StepByID("test")
	if !ok || step.ID != "test" {
		t.Errorf("expected to find step 'test'")
	}
	ids := p.StepIDs()
	if len(ids) != 3 || ids[0] != "lint" || ids[1] != "test" || ids[2] != "build" {
		t.Errorf("unexpected step IDs: %v", ids)
	}
}

func TestParse_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		errContains string
	}{
		{
			name:        "empty input",
			yaml:        "   ",
			errContains: "pipeline definition is empty",
		},
		{
			name: "unknown root key rejected strictly",
			yaml: `
name: test-pipeline
invalid_top_level_field: true
steps:
  - id: step-1
    image: alpine:latest
    run: echo hello
`,
			errContains: "invalid_top_level_field",
		},
		{
			name: "unknown step key rejected strictly",
			yaml: `
name: test-pipeline
steps:
  - id: step-1
    image: alpine:latest
    run: echo hello
    unknown_step_property: "disallowed"
`,
			errContains: "unknown_step_property",
		},
		{
			name: "missing pipeline name",
			yaml: `
steps:
  - id: step-1
    image: alpine:latest
    run: echo hello
`,
			errContains: "pipeline name is required",
		},
		{
			name: "missing steps",
			yaml: `
name: no-steps-pipeline
steps: []
`,
			errContains: "must contain at least one step",
		},
		{
			name: "missing step id",
			yaml: `
name: missing-id
steps:
  - image: alpine:latest
    run: echo hello
`,
			errContains: "step id is required",
		},
		{
			name: "invalid characters in step id",
			yaml: `
name: invalid-id
steps:
  - id: step@123!
    image: alpine:latest
    run: echo hello
`,
			errContains: "invalid step id",
		},
		{
			name: "duplicate step id",
			yaml: `
name: duplicate-id
steps:
  - id: step-1
    image: alpine:latest
    run: echo hello
  - id: step-1
    image: alpine:latest
    run: echo world
`,
			errContains: "duplicate step id",
		},
		{
			name: "missing step image",
			yaml: `
name: missing-image
steps:
  - id: step-1
    run: echo hello
`,
			errContains: "must specify a container image",
		},
		{
			name: "missing step run command",
			yaml: `
name: missing-run
steps:
  - id: step-1
    image: alpine:latest
`,
			errContains: "must specify a run command",
		},
		{
			name: "self-referencing dependency",
			yaml: `
name: self-dep
steps:
  - id: step-1
    image: alpine:latest
    run: echo hello
    depends-on: [step-1]
`,
			errContains: "cannot depend on itself",
		},
		{
			name: "non-existent dependency",
			yaml: `
name: nonexistent-dep
steps:
  - id: step-1
    image: alpine:latest
    run: echo hello
    depends-on: [nonexistent-step]
`,
			errContains: "depends on non-existent step",
		},
		{
			name: "duplicate dependency within same step",
			yaml: `
name: duplicate-dep
steps:
  - id: step-1
    image: alpine:latest
    run: echo hello
  - id: step-2
    image: alpine:latest
    run: echo world
    depends-on: [step-1, step-1]
`,
			errContains: "duplicate dependency",
		},
		{
			name: "invalid memory format",
			yaml: `
name: bad-mem
steps:
  - id: step-1
    image: alpine:latest
    run: echo hello
    resources:
      memory: "not_a_valid_memory"
`,
			errContains: "invalid memory limit",
		},
		{
			name: "invalid secret name",
			yaml: `
name: bad-secret
steps:
  - id: step-1
    image: alpine:latest
    run: echo hello
secrets:
  - "INVALID-SECRET-NAME!"
`,
			errContains: "invalid secret name",
		},
		{
			name: "duplicate secret declaration",
			yaml: `
name: dup-secret
steps:
  - id: step-1
    image: alpine:latest
    run: echo hello
secrets:
  - API_KEY
  - API_KEY
`,
			errContains: "duplicate secret declaration",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tc.yaml))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.errContains)
			}
			if !strings.Contains(err.Error(), tc.errContains) {
				t.Errorf("expected error to contain %q, but got: %v", tc.errContains, err)
			}
		})
	}
}

func TestParseMemoryBytes(t *testing.T) {
	cases := []struct {
		input    string
		expected int64
		err      bool
	}{
		{"", 0, false},
		{"512m", 512 * 1024 * 1024, false},
		{"512MB", 512 * 1024 * 1024, false},
		{"1g", 1024 * 1024 * 1024, false},
		{"2gb", 2 * 1024 * 1024 * 1024, false},
		{"256k", 256 * 1024, false},
		{"1024b", 1024, false},
		{"invalid", 0, true},
		{"-512m", 0, true},
		{"0m", 0, true},
	}

	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			res, err := ParseMemoryBytes(c.input)
			if c.err && err == nil {
				t.Errorf("input %q: expected error, got %d", c.input, res)
			}
			if !c.err && err != nil {
				t.Errorf("input %q: unexpected error: %v", c.input, err)
			}
			if !c.err && res != c.expected {
				t.Errorf("input %q: expected %d bytes, got %d", c.input, c.expected, res)
			}
		})
	}
}

