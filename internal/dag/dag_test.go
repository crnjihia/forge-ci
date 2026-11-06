// path: internal/dag/dag_test.go
package dag

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/crnji/forge-ci/internal/pipeline"
)

func makePipeline(steps ...pipeline.Step) *pipeline.Pipeline {
	return &pipeline.Pipeline{
		Name:  "test-pipeline",
		Steps: steps,
	}
}

func TestDAG_SingleStep(t *testing.T) {
	p := makePipeline(
		pipeline.Step{ID: "only-step", Image: "alpine", Run: "echo 1"},
	)
	g, err := Build(p)
	if err != nil {
		t.Fatalf("unexpected build error: %v", err)
	}

	plan, err := TopologicalSort(g)
	if err != nil {
		t.Fatalf("unexpected sort error: %v", err)
	}

	expectedLevels := [][]string{{"only-step"}}
	if !reflect.DeepEqual(plan.Levels, expectedLevels) {
		t.Errorf("expected levels %v, got %v", expectedLevels, plan.Levels)
	}
	if plan.TotalSteps() != 1 || plan.LevelCount() != 1 {
		t.Errorf("unexpected counts: total=%d, levels=%d", plan.TotalSteps(), plan.LevelCount())
	}
}

func TestDAG_LinearPipeline(t *testing.T) {
	p := makePipeline(
		pipeline.Step{ID: "step-1", Image: "alpine", Run: "echo 1"},
		pipeline.Step{ID: "step-2", Image: "alpine", Run: "echo 2", DependsOn: []string{"step-1"}},
		pipeline.Step{ID: "step-3", Image: "alpine", Run: "echo 3", DependsOn: []string{"step-2"}},
	)
	g, err := Build(p)
	if err != nil {
		t.Fatalf("unexpected build error: %v", err)
	}

	plan, err := TopologicalSort(g)
	if err != nil {
		t.Fatalf("unexpected sort error: %v", err)
	}

	expected := [][]string{
		{"step-1"},
		{"step-2"},
		{"step-3"},
	}
	if !reflect.DeepEqual(plan.Levels, expected) {
		t.Errorf("expected %v, got %v", expected, plan.Levels)
	}
	if !reflect.DeepEqual(plan.Linear, []string{"step-1", "step-2", "step-3"}) {
		t.Errorf("unexpected linear plan: %v", plan.Linear)
	}
}

func TestDAG_DiamondGraph(t *testing.T) {
	//       A
	//     /   \
	//    B     C
	//     \   /
	//       D
	p := makePipeline(
		pipeline.Step{ID: "A", Image: "alpine", Run: "echo A"},
		pipeline.Step{ID: "B", Image: "alpine", Run: "echo B", DependsOn: []string{"A"}},
		pipeline.Step{ID: "C", Image: "alpine", Run: "echo C", DependsOn: []string{"A"}},
		pipeline.Step{ID: "D", Image: "alpine", Run: "echo D", DependsOn: []string{"B", "C"}},
	)

	g, err := Build(p)
	if err != nil {
		t.Fatalf("unexpected build error: %v", err)
	}

	plan, err := TopologicalSort(g)
	if err != nil {
		t.Fatalf("unexpected sort error: %v", err)
	}

	expected := [][]string{
		{"A"},
		{"B", "C"},
		{"D"},
	}
	if !reflect.DeepEqual(plan.Levels, expected) {
		t.Errorf("expected diamond levels %v, got %v", expected, plan.Levels)
	}

	// Test Ancestors of D (should be A, B, C)
	ancestors, err := g.Ancestors("D")
	if err != nil {
		t.Fatalf("unexpected ancestor error: %v", err)
	}
	if !reflect.DeepEqual(ancestors, []string{"A", "B", "C"}) {
		t.Errorf("expected ancestors [A, B, C], got %v", ancestors)
	}

	// Test Descendants of A (should be B, C, D)
	descendants, err := g.Descendants("A")
	if err != nil {
		t.Fatalf("unexpected descendant error: %v", err)
	}
	if !reflect.DeepEqual(descendants, []string{"B", "C", "D"}) {
		t.Errorf("expected descendants [B, C, D], got %v", descendants)
	}

	// Test Direct dependencies / dependents
	if !reflect.DeepEqual(g.Dependencies("D"), []string{"B", "C"}) {
		t.Errorf("unexpected direct dependencies of D: %v", g.Dependencies("D"))
	}
	if !reflect.DeepEqual(g.Dependents("A"), []string{"B", "C"}) {
		t.Errorf("unexpected direct dependents of A: %v", g.Dependents("A"))
	}
}

func TestDAG_WideGraph(t *testing.T) {
	// Root step fans out to 15 concurrent workers, which fan into an aggregator
	workerCount := 15
	steps := make([]pipeline.Step, 0, workerCount+2)

	steps = append(steps, pipeline.Step{ID: "root", Image: "alpine", Run: "echo root"})

	workerIDs := make([]string, workerCount)
	for i := 0; i < workerCount; i++ {
		id := fmt.Sprintf("worker-%02d", i)
		workerIDs[i] = id
		steps = append(steps, pipeline.Step{
			ID:        id,
			Image:     "alpine",
			Run:       "echo worker",
			DependsOn: []string{"root"},
		})
	}

	steps = append(steps, pipeline.Step{
		ID:        "aggregate",
		Image:     "alpine",
		Run:       "echo done",
		DependsOn: workerIDs,
	})

	p := makePipeline(steps...)
	g, err := Build(p)
	if err != nil {
		t.Fatalf("unexpected build error: %v", err)
	}

	plan, err := TopologicalSort(g)
	if err != nil {
		t.Fatalf("unexpected sort error: %v", err)
	}

	if plan.LevelCount() != 3 {
		t.Fatalf("expected 3 levels, got %d", plan.LevelCount())
	}
	if len(plan.Levels[0]) != 1 || plan.Levels[0][0] != "root" {
		t.Errorf("expected level 0 to be ['root'], got %v", plan.Levels[0])
	}
	if len(plan.Levels[1]) != workerCount {
		t.Errorf("expected level 1 to contain %d workers, got %d", workerCount, len(plan.Levels[1]))
	}
	if len(plan.Levels[2]) != 1 || plan.Levels[2][0] != "aggregate" {
		t.Errorf("expected level 2 to be ['aggregate'], got %v", plan.Levels[2])
	}
}

func TestDAG_DisconnectedSubgraphs(t *testing.T) {
	// Subgraph 1: A -> B
	// Subgraph 2: X -> Y -> Z
	p := makePipeline(
		pipeline.Step{ID: "A", Image: "alpine", Run: "echo A"},
		pipeline.Step{ID: "B", Image: "alpine", Run: "echo B", DependsOn: []string{"A"}},
		pipeline.Step{ID: "X", Image: "alpine", Run: "echo X"},
		pipeline.Step{ID: "Y", Image: "alpine", Run: "echo Y", DependsOn: []string{"X"}},
		pipeline.Step{ID: "Z", Image: "alpine", Run: "echo Z", DependsOn: []string{"Y"}},
	)

	g, err := Build(p)
	if err != nil {
		t.Fatalf("unexpected build error: %v", err)
	}

	plan, err := TopologicalSort(g)
	if err != nil {
		t.Fatalf("unexpected sort error: %v", err)
	}

	expected := [][]string{
		{"A", "X"},
		{"B", "Y"},
		{"Z"},
	}
	if !reflect.DeepEqual(plan.Levels, expected) {
		t.Errorf("expected %v, got %v", expected, plan.Levels)
	}
}

func TestDAG_CycleDetection(t *testing.T) {
	tests := []struct {
		name      string
		steps     []pipeline.Step
		cycleNode string
	}{
		{
			name: "two node cycle A <-> B",
			steps: []pipeline.Step{
				{ID: "A", Image: "alpine", Run: "echo A", DependsOn: []string{"B"}},
				{ID: "B", Image: "alpine", Run: "echo B", DependsOn: []string{"A"}},
			},
			cycleNode: "A",
		},
		{
			name: "three node cycle A -> B -> C -> A",
			steps: []pipeline.Step{
				{ID: "A", Image: "alpine", Run: "echo A", DependsOn: []string{"C"}},
				{ID: "B", Image: "alpine", Run: "echo B", DependsOn: []string{"A"}},
				{ID: "C", Image: "alpine", Run: "echo C", DependsOn: []string{"B"}},
			},
			cycleNode: "A",
		},
		{
			name: "cycle in subgraph with independent branch",
			steps: []pipeline.Step{
				{ID: "init", Image: "alpine", Run: "echo init"},
				{ID: "good-branch", Image: "alpine", Run: "echo good", DependsOn: []string{"init"}},
				{ID: "bad-1", Image: "alpine", Run: "echo 1", DependsOn: []string{"init", "bad-2"}},
				{ID: "bad-2", Image: "alpine", Run: "echo 2", DependsOn: []string{"bad-1"}},
			},
			cycleNode: "bad-",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := makePipeline(tc.steps...)
			g, err := Build(p)
			if err != nil {
				t.Fatalf("unexpected build error: %v", err)
			}

			plan, err := TopologicalSort(g)
			if err == nil {
				t.Fatalf("expected cycle error, but sort succeeded with plan: %v", plan)
			}

			cycleErr, ok := err.(*CycleError)
			if !ok {
				t.Fatalf("expected *CycleError, got %T: %v", err, err)
			}

			errMsg := cycleErr.Error()
			if !strings.Contains(errMsg, "cyclical dependency detected") {
				t.Errorf("error message missing expected prefix: %s", errMsg)
			}
			if !strings.Contains(errMsg, tc.cycleNode) {
				t.Errorf("error message missing node %q: %s", tc.cycleNode, errMsg)
			}
		})
	}
}

func TestDAG_UnknownStepErrors(t *testing.T) {
	p := makePipeline(
		pipeline.Step{ID: "step-1", Image: "alpine", Run: "echo 1", DependsOn: []string{"ghost-step"}},
	)
	_, err := Build(p)
	if err == nil {
		t.Fatalf("expected error for unknown step reference, got nil")
	}
	if !strings.Contains(err.Error(), "unknown step \"ghost-step\"") {
		t.Errorf("unexpected error message: %v", err)
	}
}

