package model

import "github.com/gsoultan/storm"

// SimulationCase is one saved test case for a process: what it starts with,
// how the outside world answers it, and what it must end with.
//
// Cases are kept by process key rather than by definition row, as retention
// periods are: a case is written against the process, and the point of keeping
// it is to run it against the next version.
type SimulationCase struct {
	storm.Model

	Project    Project
	ProcessKey string
	Name       string

	// Scenario is the case as the simulation endpoint takes it — variables,
	// answers and seed — and Expect is what the run must end with. JSON, because
	// both are read and written whole and never queried into.
	Scenario storm.JSON
	Expect   storm.JSON
}

func (c *SimulationCase) Schema(t *storm.Table) {
	t.Name("simulation_cases")
	t.Col(&c.ProcessKey).Size(255)
	t.Col(&c.Name).Size(255)
	// A process's cases, oldest first, which is the order the designer lists them.
	t.Index(&c.Project, &c.ProcessKey, &c.CreatedAt, &c.ID).Named("ix_simulation_cases_process")
}
