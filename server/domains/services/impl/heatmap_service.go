package impl

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
)

// heatmapService implements HeatmapReader and SLAReporter by deriving statistics
// from audit logs and active process instance tokens.
type heatmapService struct {
	repo repositories.Repository
}

// NewHeatmapService creates a new heatmapService.
func NewHeatmapService(repo repositories.Repository) *heatmapService {
	return &heatmapService{repo: repo}
}

// GetHeatmap aggregates node statistics across all instances of the given definition.
func (s *heatmapService) GetHeatmap(ctx context.Context, definitionID uuid.UUID) ([]entities.HeatmapNode, error) {
	instances, err := s.loadInstancesForDefinition(ctx, definitionID)
	if err != nil {
		return nil, err
	}
	stats := s.aggregateNodeStats(instances)
	return s.buildHeatmapNodes(stats), nil
}

// ListBreachedSLAs returns SLA entries where the task due date has passed.
func (s *heatmapService) ListBreachedSLAs(ctx context.Context, projectID uuid.UUID) ([]entities.SLAEntry, error) {
	tasks, err := s.repo.Task().ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var entries []entities.SLAEntry
	now := time.Now()
	for _, t := range tasks {
		task := adapters.TaskEntityAdapter{Model: t}.ToEntity()
		entry := buildSLAEntry(task, now)
		if entry.Status == entities.SLAStatusBreached {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// GetInstanceSLA returns SLA compliance details for every active task of an instance.
func (s *heatmapService) GetInstanceSLA(ctx context.Context, instanceID uuid.UUID) ([]entities.SLAEntry, error) {
	tasks, err := s.repo.Task().ListByInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	entries := make([]entities.SLAEntry, 0, len(tasks))
	for _, t := range tasks {
		task := adapters.TaskEntityAdapter{Model: t}.ToEntity()
		entries = append(entries, buildSLAEntry(task, now))
	}
	return entries, nil
}

// nodeStats holds running aggregates for a single BPMN node.
type nodeStats struct {
	name            string
	activeCount     int
	completedCount  int
	totalDurationMs int64
	maxDurationMs   int64
	lastActivity    time.Time
}

func (s *heatmapService) loadInstancesForDefinition(ctx context.Context, definitionID uuid.UUID) ([]entities.ProcessInstance, error) {
	ms, err := s.repo.Process().ListByDefinition(ctx, definitionID)
	if err != nil {
		return nil, err
	}
	result := make([]entities.ProcessInstance, len(ms))
	for i, m := range ms {
		result[i] = adapters.InstanceEntityAdapter{Model: m}.ToEntity()
	}
	return result, nil
}

func (s *heatmapService) aggregateNodeStats(instances []entities.ProcessInstance) map[string]*nodeStats {
	stats := make(map[string]*nodeStats)
	for _, inst := range instances {
		for _, token := range inst.Tokens {
			if token.Node == nil {
				continue
			}
			ns := getOrCreate(stats, token.Node.ID)
			if ns.name == "" && token.Node.Name != "" {
				ns.name = token.Node.Name
			}
			ns.activeCount++
			durMs := time.Since(token.CreatedAt).Milliseconds()
			ns.totalDurationMs += durMs
			if durMs > ns.maxDurationMs {
				ns.maxDurationMs = durMs
			}
			if token.CreatedAt.After(ns.lastActivity) {
				ns.lastActivity = token.CreatedAt
			}
		}
		for _, n := range inst.CompletedNodes {
			if n == nil {
				continue
			}
			ns := getOrCreate(stats, n.ID)
			if ns.name == "" && n.Name != "" {
				ns.name = n.Name
			}
			ns.completedCount++
		}
	}
	return stats
}

func (s *heatmapService) buildHeatmapNodes(stats map[string]*nodeStats) []entities.HeatmapNode {
	nodes := make([]entities.HeatmapNode, 0, len(stats))
	for nodeID, ns := range stats {
		var avg int64
		if ns.activeCount > 0 {
			avg = ns.totalDurationMs / int64(ns.activeCount)
		}
		nodes = append(nodes, entities.HeatmapNode{
			Node:           &entities.Node{ID: nodeID, Name: ns.name},
			ActiveCount:    ns.activeCount,
			CompletedCount: ns.completedCount,
			AvgDurationMs:  avg,
			MaxDurationMs:  ns.maxDurationMs,
			LastActivity:   ns.lastActivity,
		})
	}
	return nodes
}

func getOrCreate(stats map[string]*nodeStats, nodeID string) *nodeStats {
	if ns, ok := stats[nodeID]; ok {
		return ns
	}
	ns := &nodeStats{}
	stats[nodeID] = ns
	return ns
}

func buildSLAEntry(task entities.Task, now time.Time) entities.SLAEntry {
	entry := entities.SLAEntry{
		Instance: task.Instance,
		Node:     task.Node,
	}
	if task.DueDate != nil {
		entry.DueAt = *task.DueDate
		durationMs := now.Sub(task.CreatedAt).Milliseconds()
		entry.DurationMs = durationMs
		entry.Status = classifySLAStatus(task, now)
	} else {
		entry.Status = entities.SLAStatusOnTrack
	}
	return entry
}

func classifySLAStatus(task entities.Task, now time.Time) entities.SLAStatus {
	if task.DueDate == nil {
		return entities.SLAStatusOnTrack
	}
	timeLeft := task.DueDate.Sub(now)
	switch {
	case timeLeft < 0:
		return entities.SLAStatusBreached
	case timeLeft < 2*time.Hour:
		return entities.SLAStatusAtRisk
	default:
		return entities.SLAStatusOnTrack
	}
}
