// path: internal/pipeline/model.go
package pipeline

import (
	"fmt"
	"strconv"
	"strings"
)

// Pipeline represents a declarative CI/CD pipeline definition.
type Pipeline struct {
	Name    string            `yaml:"name"`
	Env     map[string]string `yaml:"env,omitempty"`
	Steps   []Step            `yaml:"steps"`
	Secrets []string          `yaml:"secrets,omitempty"`
}

// Resources represents CPU and memory limits for a containerized step.
type Resources struct {
	CPU    float64 `yaml:"cpu,omitempty"`
	Memory string  `yaml:"memory,omitempty"`
}

// Step represents a single isolated execution step inside a container.
type Step struct {
	ID              string            `yaml:"id"`
	Name            string            `yaml:"name,omitempty"`
	Image           string            `yaml:"image"`
	Run             string            `yaml:"run"`
	DependsOn       []string          `yaml:"depends-on,omitempty"`
	Resources       *Resources        `yaml:"resources,omitempty"`
	Artifacts       []string          `yaml:"artifacts,omitempty"`
	Env             map[string]string `yaml:"env,omitempty"`
	ContinueOnError bool              `yaml:"continue-on-error,omitempty"`
}

// StepByID finds and returns a pointer to the Step matching the given ID.
func (p *Pipeline) StepByID(id string) (*Step, bool) {
	for i := range p.Steps {
		if p.Steps[i].ID == id {
			return &p.Steps[i], true
		}
	}
	return nil, false
}

// StepIDs returns all step identifiers in definition order.
func (p *Pipeline) StepIDs() []string {
	ids := make([]string, len(p.Steps))
	for i, s := range p.Steps {
		ids[i] = s.ID
	}
	return ids
}

// ParseMemoryBytes converts human-readable memory strings (e.g., "512m", "1g", "256k")
// into byte integers. Returns an error if the format is invalid or value <= 0.
func ParseMemoryBytes(mem string) (int64, error) {
	s := strings.TrimSpace(mem)
	if s == "" {
		return 0, nil
	}

	lower := strings.ToLower(s)
	multiplier := int64(1)

	switch {
	case strings.HasSuffix(lower, "gb") || strings.HasSuffix(lower, "g"):
		multiplier = 1024 * 1024 * 1024
		lower = strings.TrimSuffix(strings.TrimSuffix(lower, "b"), "g")
	case strings.HasSuffix(lower, "mb") || strings.HasSuffix(lower, "m"):
		multiplier = 1024 * 1024
		lower = strings.TrimSuffix(strings.TrimSuffix(lower, "b"), "m")
	case strings.HasSuffix(lower, "kb") || strings.HasSuffix(lower, "k"):
		multiplier = 1024
		lower = strings.TrimSuffix(strings.TrimSuffix(lower, "b"), "k")
	case strings.HasSuffix(lower, "b"):
		multiplier = 1
		lower = strings.TrimSuffix(lower, "b")
	}

	val, err := strconv.ParseInt(strings.TrimSpace(lower), 10, 64)
	if err != nil || val <= 0 {
		return 0, fmt.Errorf("invalid memory limit %q: must be a positive integer with optional unit (e.g. 512m, 1g)", mem)
	}

	return val * multiplier, nil
}
