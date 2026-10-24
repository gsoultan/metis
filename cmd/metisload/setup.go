package main

import (
	"context"
	"fmt"
)

// definitionKey is the process every virtual user runs: a request that one
// person approves. It is the shape most real work has — a start, a person, an
// end — and it exercises every write a user task costs: the instance, the
// token, the task, its claim and its completion, and the audit of each.
const definitionKey = "load-approval"

const reviewForm = `[{"id":"approved","label":"Approved","type":"boolean"},` +
	`{"id":"approverNote","label":"Note","type":"textarea"}]`

// signIn returns the token every virtual user carries.
func (c *client) signIn(ctx context.Context, username, password string) error {
	var answer struct {
		Token string `json:"token"`
	}
	err := c.call(ctx, "POST", "/api/v1/login", map[string]string{
		"username": username, "password": password,
	}, &answer)
	if err != nil {
		return fmt.Errorf("signing in as %s: %w", username, err)
	}
	c.token = answer.Token
	return nil
}

// firstProject is where the load runs when no project is named.
func (c *client) firstProject(ctx context.Context) (string, error) {
	var answer struct {
		Projects []struct {
			ID string `json:"id"`
		} `json:"projects"`
	}
	if err := c.call(ctx, "GET", "/api/v1/projects", nil, &answer); err != nil {
		return "", fmt.Errorf("listing projects: %w", err)
	}
	if len(answer.Projects) == 0 {
		return "", fmt.Errorf("the signed-in user has no project to run the load in")
	}
	return answer.Projects[0].ID, nil
}

// importDefinition files the load definition in the project. A second import
// files a new version rather than failing, so a rerun needs no cleanup.
func (c *client) importDefinition(ctx context.Context, projectID, assignee string) error {
	definition := map[string]any{
		"key":     definitionKey,
		"name":    "Load test approval",
		"project": map[string]string{"id": projectID},
		"nodes": []map[string]any{
			{"id": "start", "name": "Request submitted", "type": "startEvent"},
			{
				"id": "review", "name": "Approve the request", "type": "userTask",
				// Offered rather than assigned, so each request is claimed
				// before it is completed, as an inbox does it.
				"candidate_users": []map[string]string{{"username": assignee}},
				"properties":      map[string]any{"form_definition": reviewForm},
			},
			{"id": "end", "name": "Request decided", "type": "endEvent"},
		},
		"flows": []map[string]any{
			{"id": "f1", "source_ref": "start", "target_ref": "review"},
			{"id": "f2", "source_ref": "review", "target_ref": "end"},
		},
	}
	if err := c.call(ctx, "POST", "/api/v1/definitions", map[string]any{"definition": definition}, nil); err != nil {
		return fmt.Errorf("importing %s: %w", definitionKey, err)
	}
	return nil
}
