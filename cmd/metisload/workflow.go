package main

import (
	"context"
	"fmt"
	"net/url"
)

// The calls one virtual user makes, named as the report names them.
const (
	opStart    = "start instance"
	opFindTask = "find task"
	opClaim    = "claim task"
	opComplete = "complete task"
	opList     = "list instances"
)

var operations = []string{opStart, opFindTask, opClaim, opComplete, opList}

const listPageSize = 25

// startInstance starts the load definition and returns the new instance's id.
func (c *client) startInstance(ctx context.Context, projectID string, sequence int) (string, error) {
	var answer struct {
		InstanceID string `json:"instanceId"`
	}
	err := c.call(ctx, "POST", "/api/v1/process.ProcessService/StartProcess", map[string]any{
		"projectId":     projectID,
		"definitionKey": definitionKey,
		"variables": map[string]any{
			"amount":      float64(100 + sequence%900),
			"currency":    "GBP",
			"description": fmt.Sprintf("Load request %d", sequence),
			"submittedBy": c.forwardedFor,
		},
	}, &answer)
	if err == nil && answer.InstanceID == "" {
		err = fmt.Errorf("started, but no instance id came back")
	}
	return answer.InstanceID, err
}

// findTask returns the one open task of an instance: what an inbox reads before
// somebody opens the work.
func (c *client) findTask(ctx context.Context, projectID, instanceID string) (string, error) {
	var answer struct {
		Tasks []struct {
			ID string `json:"id"`
		} `json:"tasks"`
	}
	query := url.Values{"project_id": {projectID}, "instance_id": {instanceID}}
	if err := c.call(ctx, "GET", "/api/v1/tasks?"+query.Encode(), nil, &answer); err != nil {
		return "", err
	}
	if len(answer.Tasks) != 1 {
		return "", fmt.Errorf("instance %s has %d open tasks, want 1", instanceID, len(answer.Tasks))
	}
	return answer.Tasks[0].ID, nil
}

func (c *client) claimTask(ctx context.Context, taskID string) error {
	return c.call(ctx, "POST", "/api/v1/process.TaskService/ClaimTask", map[string]any{"id": taskID}, nil)
}

func (c *client) completeTask(ctx context.Context, taskID string) error {
	return c.call(ctx, "POST", "/api/v1/process.TaskService/CompleteTask", map[string]any{
		"id":        taskID,
		"variables": map[string]any{"approved": true, "approverNote": "Approved under load"},
	}, nil)
}

// listInstances reads the first page of the project's instances, which is the
// screen an operator keeps open while all of this happens.
func (c *client) listInstances(ctx context.Context, projectID string) error {
	return c.call(ctx, "POST", "/api/v1/process.ProcessService/ListInstances", map[string]any{
		"projectId": projectID,
		"page":      map[string]any{"page": 1, "pageSize": listPageSize},
	}, nil)
}
