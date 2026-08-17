package gobpm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// ImportDefinition deploys BPMN 2.0 XML into a project and returns the new
// definition's ID. Redeploying the same process key creates the next version;
// running instances keep the version they started with.
func (c *Client) ImportDefinition(ctx context.Context, projectID string, xml []byte) (string, error) {
	if projectID == "" {
		return "", errors.New("gobpm: ImportDefinition needs a project ID — a definition without a project is invisible to its own organization")
	}
	var out struct {
		ID string `json:"id"`
	}
	err := c.do(ctx, http.MethodPost, "/api/v1/definitions/import", map[string]any{
		"project_id": projectID,
		"xml":        xml, // marshals to base64, which is what the server decodes
	}, &out)
	if err != nil {
		return "", err
	}
	return out.ID, nil
}

// ExportDefinition returns a deployed definition as BPMN 2.0 XML.
func (c *Client) ExportDefinition(ctx context.Context, definitionID string) ([]byte, error) {
	var out struct {
		XML []byte `json:"xml"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/definitions/"+url.PathEscape(definitionID)+"/export", nil, &out)
	return out.XML, err
}

// StartProcess starts an instance of the definition deployed under
// definitionKey in the given project, seeded with variables, and returns the
// instance ID. The latest deployed version is used.
func (c *Client) StartProcess(ctx context.Context, projectID, definitionKey string, variables Variables) (string, error) {
	var out struct {
		InstanceID string `json:"instance_id"`
	}
	err := c.do(ctx, http.MethodPost, "/api/v1/process/start", map[string]any{
		"project_id":     projectID,
		"definition_key": definitionKey,
		"variables":      variables,
	}, &out)
	if err != nil {
		return "", err
	}
	return out.InstanceID, nil
}

// GetInstance returns one process instance, including its current variables
// and status.
func (c *Client) GetInstance(ctx context.Context, instanceID string) (*ProcessInstance, error) {
	var out struct {
		Instance ProcessInstance `json:"instance"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/instances/"+url.PathEscape(instanceID), nil, &out)
	if err != nil {
		return nil, err
	}
	return &out.Instance, nil
}

// GetTimeline returns an instance's audit trail — the plain-language record
// of what happened, in order.
func (c *Client) GetTimeline(ctx context.Context, instanceID string) ([]AuditEntry, error) {
	var out struct {
		Entries []AuditEntry `json:"entries"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/instances/"+url.PathEscape(instanceID)+"/audit", nil, &out)
	return out.Entries, err
}

// SendMessage correlates a message into whatever instance is waiting on it.
//
// correlationKey selects which instance when several wait on the same message
// name. Pass "" only when the message name alone is unambiguous — an empty
// key matches every waiting subscription for that name.
func (c *Client) SendMessage(ctx context.Context, projectID, messageName, correlationKey string, variables Variables) error {
	return c.do(ctx, http.MethodPost, "/api/v1/processes/message", map[string]any{
		"project_id":      projectID,
		"message_name":    messageName,
		"correlation_key": correlationKey,
		"variables":       variables,
	}, nil)
}

// BroadcastSignal delivers a signal to every instance in the project waiting
// on it. Unlike a message, a signal has no single addressee.
func (c *Client) BroadcastSignal(ctx context.Context, projectID, signalName string, variables Variables) error {
	return c.do(ctx, http.MethodPost, "/api/v1/processes/signal", map[string]any{
		"project_id":  projectID,
		"signal_name": signalName,
		"variables":   variables,
	}, nil)
}

// ListTasksOptions filters and pages a task listing. The zero value lists the
// first page at the server's default size.
type ListTasksOptions struct {
	ProjectID string
	Page      int
	PageSize  int
}

// ListTasks lists human tasks across the caller's organization, newest first.
func (c *Client) ListTasks(ctx context.Context, opts ListTasksOptions) ([]Task, *PageInfo, error) {
	query := url.Values{}
	if opts.ProjectID != "" {
		query.Set("project_id", opts.ProjectID)
	}
	if opts.Page > 0 {
		query.Set("page", fmt.Sprint(opts.Page))
	}
	if opts.PageSize > 0 {
		query.Set("page_size", fmt.Sprint(opts.PageSize))
	}
	path := "/api/v1/tasks"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}

	var out struct {
		Page  *PageInfo `json:"page"`
		Tasks []Task    `json:"tasks"`
	}
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out.Tasks, out.Page, err
}

// GetTask returns one human task.
func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error) {
	var out struct {
		Task Task `json:"task"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/tasks/"+url.PathEscape(taskID), nil, &out)
	if err != nil {
		return nil, err
	}
	return &out.Task, nil
}

// ClaimTask takes a task for userID, so nobody else works it in parallel.
func (c *Client) ClaimTask(ctx context.Context, taskID, userID string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/tasks/"+url.PathEscape(taskID)+"/claim", map[string]any{
		"user_id": userID,
	}, nil)
}

// CompleteTask finishes a task as userID, writing variables back into the
// process, and the instance moves on.
func (c *Client) CompleteTask(ctx context.Context, taskID, userID string, variables Variables) error {
	return c.do(ctx, http.MethodPost, "/api/v1/tasks/"+url.PathEscape(taskID)+"/complete", map[string]any{
		"user_id":   userID,
		"variables": variables,
	}, nil)
}

// ProjectRef identifies a project — the container definitions deploy into.
type ProjectRef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ListProjects lists the projects the caller's organization owns. Most other
// calls take a project ID; this is where integrations discover it.
func (c *Client) ListProjects(ctx context.Context) ([]ProjectRef, error) {
	var out struct {
		Projects []ProjectRef `json:"projects"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/projects", nil, &out)
	return out.Projects, err
}
