// path: internal/pipeline/validator.go
package pipeline

import (
	"fmt"
	"regexp"

	"gopkg.in/yaml.v3"
)

var (
	stepIDRegex   = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	secretIDRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// Validate performs deep semantic verification on a parsed pipeline.
// If an AST node is provided, line numbers are attached to errors.
func Validate(p *Pipeline, rootNode *yaml.Node) error {
	if p.Name == "" {
		line := findLineForField(rootNode, "name", 1)
		return &ParseError{Line: line, Field: "name", Message: "pipeline name is required"}
	}

	if len(p.Steps) == 0 {
		line := findLineForField(rootNode, "steps", 1)
		return &ParseError{Line: line, Field: "steps", Message: "pipeline must contain at least one step"}
	}

	knownStepIDs := make(map[string]bool, len(p.Steps))
	for _, step := range p.Steps {
		stepLine := findLineForStepID(rootNode, step.ID)

		if step.ID == "" {
			return &ParseError{Line: stepLine, Field: "steps.id", Message: "step id is required"}
		}

		if !stepIDRegex.MatchString(step.ID) {
			return &ParseError{
				Line:    stepLine,
				Field:   "steps.id",
				Message: fmt.Sprintf("invalid step id %q: must contain only letters, numbers, dashes, and underscores", step.ID),
			}
		}

		if knownStepIDs[step.ID] {
			return &ParseError{
				Line:    stepLine,
				Field:   "steps.id",
				Message: fmt.Sprintf("duplicate step id %q", step.ID),
			}
		}
		knownStepIDs[step.ID] = true

		if step.Image == "" {
			return &ParseError{
				Line:    stepLine,
				Field:   "steps.image",
				Message: fmt.Sprintf("step %q must specify a container image", step.ID),
			}
		}

		if step.Run == "" {
			return &ParseError{
				Line:    stepLine,
				Field:   "steps.run",
				Message: fmt.Sprintf("step %q must specify a run command", step.ID),
			}
		}

		if step.Resources != nil {
			if step.Resources.CPU < 0 || (step.Resources.CPU == 0 && step.Resources.CPU != 0.0) {
				return &ParseError{
					Line:    stepLine,
					Field:   "steps.resources.cpu",
					Message: fmt.Sprintf("step %q cpu limit must be greater than 0", step.ID),
				}
			}
			if step.Resources.Memory != "" {
				if _, err := ParseMemoryBytes(step.Resources.Memory); err != nil {
					return &ParseError{
						Line:    stepLine,
						Field:   "steps.resources.memory",
						Message: fmt.Sprintf("step %q: %s", step.ID, err.Error()),
					}
				}
			}
		}
	}

	// Validate step dependency references
	for _, step := range p.Steps {
		stepLine := findLineForStepID(rootNode, step.ID)
		depSeen := make(map[string]bool, len(step.DependsOn))

		for _, dep := range step.DependsOn {
			if dep == step.ID {
				return &ParseError{
					Line:    stepLine,
					Field:   "steps.depends-on",
					Message: fmt.Sprintf("step %q cannot depend on itself", step.ID),
				}
			}

			if !knownStepIDs[dep] {
				return &ParseError{
					Line:    stepLine,
					Field:   "steps.depends-on",
					Message: fmt.Sprintf("step %q depends on non-existent step %q", step.ID, dep),
				}
			}

			if depSeen[dep] {
				return &ParseError{
					Line:    stepLine,
					Field:   "steps.depends-on",
					Message: fmt.Sprintf("step %q has duplicate dependency %q", step.ID, dep),
				}
			}
			depSeen[dep] = true
		}
	}

	// Validate secrets
	secretSeen := make(map[string]bool, len(p.Secrets))
	for _, secret := range p.Secrets {
		secretLine := findLineForField(rootNode, "secrets", 1)
		if !secretIDRegex.MatchString(secret) {
			return &ParseError{
				Line:    secretLine,
				Field:   "secrets",
				Message: fmt.Sprintf("invalid secret name %q: must be a valid environment identifier", secret),
			}
		}
		if secretSeen[secret] {
			return &ParseError{
				Line:    secretLine,
				Field:   "secrets",
				Message: fmt.Sprintf("duplicate secret declaration %q", secret),
			}
		}
		secretSeen[secret] = true
	}

	return nil
}

// findLineForField searches the yaml.Node mapping for a top-level key to extract its line number.
func findLineForField(node *yaml.Node, key string, fallback int) int {
	if node == nil {
		return fallback
	}
	mapping := node
	if mapping.Kind == yaml.DocumentNode && len(mapping.Content) > 0 {
		mapping = mapping.Content[0]
	}
	if mapping.Kind != yaml.MappingNode {
		return fallback
	}
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i].Line
		}
	}
	return fallback
}

// findLineForStepID traverses the AST to locate the line where step with given ID is defined.
func findLineForStepID(node *yaml.Node, id string) int {
	if node == nil || id == "" {
		return 1
	}
	mapping := node
	if mapping.Kind == yaml.DocumentNode && len(mapping.Content) > 0 {
		mapping = mapping.Content[0]
	}
	if mapping.Kind != yaml.MappingNode {
		return 1
	}
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == "steps" {
			stepsNode := mapping.Content[i+1]
			if stepsNode.Kind == yaml.SequenceNode {
				for _, stepMap := range stepsNode.Content {
					if stepMap.Kind == yaml.MappingNode {
						for k := 0; k < len(stepMap.Content)-1; k += 2 {
							if stepMap.Content[k].Value == "id" && stepMap.Content[k+1].Value == id {
								return stepMap.Line
							}
						}
					}
				}
			}
		}
	}
	return 1
}

