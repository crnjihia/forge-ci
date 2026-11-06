// path: internal/dag/graph.go
package dag

import (
	"fmt"
	"sort"

	"github.com/crnji/forge-ci/internal/pipeline"
)

// Graph represents a directed acyclic graph of pipeline steps.
type Graph struct {
	steps      map[string]*pipeline.Step // step ID -> Step definition
	deps       map[string][]string        // step ID -> slice of IDs it depends on (in-edges)
	dependents map[string][]string        // step ID -> slice of IDs that depend on it (out-edges)
	inDegree   map[string]int             // step ID -> number of prerequisites
	order      []string                   // original declaration order of step IDs
}

// Build constructs a dependency graph from a parsed pipeline.
// Returns an error if an unknown dependency reference is found.
func Build(p *pipeline.Pipeline) (*Graph, error) {
	if p == nil {
		return nil, fmt.Errorf("pipeline cannot be nil")
	}

	g := &Graph{
		steps:      make(map[string]*pipeline.Step, len(p.Steps)),
		deps:       make(map[string][]string, len(p.Steps)),
		dependents: make(map[string][]string, len(p.Steps)),
		inDegree:   make(map[string]int, len(p.Steps)),
		order:      make([]string, 0, len(p.Steps)),
	}

	for i := range p.Steps {
		step := &p.Steps[i]
		g.steps[step.ID] = step
		g.deps[step.ID] = make([]string, 0, len(step.DependsOn))
		g.dependents[step.ID] = make([]string, 0)
		g.inDegree[step.ID] = 0
		g.order = append(g.order, step.ID)
	}

	for _, step := range p.Steps {
		for _, depID := range step.DependsOn {
			if _, exists := g.steps[depID]; !exists {
				return nil, fmt.Errorf("step %q depends on unknown step %q", step.ID, depID)
			}
			g.deps[step.ID] = append(g.deps[step.ID], depID)
			g.dependents[depID] = append(g.dependents[depID], step.ID)
			g.inDegree[step.ID]++
		}
	}

	return g, nil
}

// Step returns the pipeline step for the given ID.
func (g *Graph) Step(id string) (*pipeline.Step, bool) {
	s, ok := g.steps[id]
	return s, ok
}

// Dependencies returns the direct prerequisites that stepID depends on.
func (g *Graph) Dependencies(stepID string) []string {
	if d, ok := g.deps[stepID]; ok {
		out := make([]string, len(d))
		copy(out, d)
		return out
	}
	return nil
}

// Dependents returns the direct steps that depend on stepID.
func (g *Graph) Dependents(stepID string) []string {
	if d, ok := g.dependents[stepID]; ok {
		out := make([]string, len(d))
		copy(out, d)
		return out
	}
	return nil
}

// StepIDs returns all step IDs in the original declaration order.
func (g *Graph) StepIDs() []string {
	out := make([]string, len(g.order))
	copy(out, g.order)
	return out
}

// Ancestors returns all transitive prerequisites that must complete before stepID.
func (g *Graph) Ancestors(stepID string) ([]string, error) {
	if _, ok := g.steps[stepID]; !ok {
		return nil, fmt.Errorf("step %q does not exist", stepID)
	}

	visited := make(map[string]bool)
	var result []string

	var dfs func(curr string)
	dfs = func(curr string) {
		for _, dep := range g.deps[curr] {
			if !visited[dep] {
				visited[dep] = true
				result = append(result, dep)
				dfs(dep)
			}
		}
	}

	dfs(stepID)
	sort.Strings(result)
	return result, nil
}

// Descendants returns all transitive steps that depend on stepID.
func (g *Graph) Descendants(stepID string) ([]string, error) {
	if _, ok := g.steps[stepID]; !ok {
		return nil, fmt.Errorf("step %q does not exist", stepID)
	}

	visited := make(map[string]bool)
	var result []string

	var dfs func(curr string)
	dfs = func(curr string) {
		for _, dep := range g.dependents[curr] {
			if !visited[dep] {
				visited[dep] = true
				result = append(result, dep)
				dfs(dep)
			}
		}
	}

	dfs(stepID)
	sort.Strings(result)
	return result, nil
}

// FindCycle traces a directed cycle in the graph using depth-first search.
// Returns the slice of step IDs forming the cycle (e.g., ["A", "B", "C", "A"]),
// or nil if the graph is strictly acyclic.
func (g *Graph) FindCycle() []string {
	const (
		white = 0 // unvisited
		gray  = 1 // currently visiting in call stack
		black = 2 // finished
	)

	color := make(map[string]int, len(g.steps))
	parent := make(map[string]string, len(g.steps))

	for id := range g.steps {
		color[id] = white
	}

	var cycle []string

	var visit func(u string) bool
	visit = func(u string) bool {
		color[u] = gray
		for _, v := range g.dependents[u] {
			if color[v] == gray {
				// Cycle detected: backtrack from u to v
				cycle = append(cycle, v)
				curr := u
				for curr != v {
					cycle = append(cycle, curr)
					curr = parent[curr]
				}
				cycle = append(cycle, v)
				// Reverse so cycle reads u -> v -> ... -> u
				for i, j := 0, len(cycle)-1; i < j; i, j = i+1, j-1 {
					cycle[i], cycle[j] = cycle[j], cycle[i]
				}
				return true
			}
			if color[v] == white {
				parent[v] = u
				if visit(v) {
					return true
				}
			}
		}
		color[u] = black
		return false
	}

	for _, id := range g.order {
		if color[id] == white {
			if visit(id) {
				return cycle
			}
		}
	}

	return nil
}

