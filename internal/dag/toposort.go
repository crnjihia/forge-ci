// path: internal/dag/toposort.go
package dag

import (
	"fmt"
	"strings"
)

// CycleError is returned when a directed cycle is detected in the pipeline DAG.
type CycleError struct {
	Path []string
}

func (e *CycleError) Error() string {
	if len(e.Path) > 0 {
		return fmt.Sprintf("cyclical dependency detected in pipeline: %s", strings.Join(e.Path, " -> "))
	}
	return "cyclical dependency detected in pipeline"
}

// ExecutionPlan contains the topological levels for parallel step execution.
type ExecutionPlan struct {
	// Levels holds step IDs partitioned into sequential tiers.
	// Steps within the same level have all prerequisites satisfied and can run in parallel.
	Levels [][]string

	// Linear is the flattened topological order of step IDs.
	Linear []string
}

// TotalSteps returns the total count of distinct steps in the plan.
func (p *ExecutionPlan) TotalSteps() int {
	return len(p.Linear)
}

// LevelCount returns the number of sequential execution stages.
func (p *ExecutionPlan) LevelCount() int {
	return len(p.Levels)
}

// TopologicalSort computes execution tiers using Kahn's BFS algorithm.
// If the graph contains a cycle, a CycleError detailing the cycle path is returned.
func TopologicalSort(g *Graph) (*ExecutionPlan, error) {
	if g == nil {
		return nil, fmt.Errorf("graph cannot be nil")
	}

	if len(g.steps) == 0 {
		return &ExecutionPlan{
			Levels: [][]string{},
			Linear: []string{},
		}, nil
	}

	// Working in-degree map
	inDegree := make(map[string]int, len(g.steps))
	for id, deg := range g.inDegree {
		inDegree[id] = deg
	}

	// Order lookup helper for deterministic tier sorting
	orderIndex := make(map[string]int, len(g.order))
	for i, id := range g.order {
		orderIndex[id] = i
	}

	sortByOriginalOrder := func(slice []string) {
		for i := 0; i < len(slice)-1; i++ {
			for j := i + 1; j < len(slice); j++ {
				if orderIndex[slice[i]] > orderIndex[slice[j]] {
					slice[i], slice[j] = slice[j], slice[i]
				}
			}
		}
	}

	// Find initial frontier (in-degree 0: steps with no dependencies)
	currentLevel := make([]string, 0)
	for _, id := range g.order {
		if inDegree[id] == 0 {
			currentLevel = append(currentLevel, id)
		}
	}

	var levels [][]string
	var linear []string
	processed := 0

	for len(currentLevel) > 0 {
		sortByOriginalOrder(currentLevel)
		levels = append(levels, currentLevel)
		linear = append(linear, currentLevel...)
		processed += len(currentLevel)

		nextLevel := make([]string, 0)
		for _, u := range currentLevel {
			for _, v := range g.dependents[u] {
				inDegree[v]--
				if inDegree[v] == 0 {
					nextLevel = append(nextLevel, v)
				}
			}
		}

		currentLevel = nextLevel
	}

	// If fewer nodes were processed than exist, there is at least one cycle.
	if processed < len(g.steps) {
		cycle := g.FindCycle()
		return nil, &CycleError{Path: cycle}
	}

	return &ExecutionPlan{
		Levels: levels,
		Linear: linear,
	}, nil
}

