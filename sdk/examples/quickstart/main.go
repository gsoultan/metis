// Command quickstart walks the whole integration surface against a live
// server: deploy a definition, start an instance, serve its external task
// with a worker, complete its human task, and read the timeline.
//
// It doubles as the SDK's end-to-end proof — everything it prints was really
// answered by a running engine, not a mock.
//
//	GOBPM_URL=http://localhost:8080 \
//	GOBPM_USERNAME=admin GOBPM_PASSWORD=secret \
//	GOBPM_PROJECT="Default Project" go run ./examples/quickstart
package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"time"

	gobpm "github.com/gsoultan/gobpm/sdk"
)

// refundProcess is a complete, runnable definition: an external task a worker
// serves, then a human approval, then done.
const refundProcess = `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL" id="defs">
  <process id="refund" name="Refund a customer">
    <startEvent id="start"/>
    <serviceTask id="charge" name="Reverse the charge" topic="reverse-charge"/>
    <userTask id="approve" name="Approve the refund"/>
    <endEvent id="end"/>
    <sequenceFlow id="f1" sourceRef="start" targetRef="charge"/>
    <sequenceFlow id="f2" sourceRef="charge" targetRef="approve"/>
    <sequenceFlow id="f3" sourceRef="approve" targetRef="end"/>
  </process>
</definitions>`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "quickstart:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	baseURL := cmp.Or(os.Getenv("GOBPM_URL"), "http://localhost:8080")
	username := cmp.Or(os.Getenv("GOBPM_USERNAME"), "admin")
	password := os.Getenv("GOBPM_PASSWORD")
	projectName := cmp.Or(os.Getenv("GOBPM_PROJECT"), "Default Project")

	client := gobpm.NewClient(baseURL)

	// 1. Authenticate.
	if err := client.Login(ctx, username, password); err != nil {
		return fmt.Errorf("login as %s: %w", username, err)
	}
	fmt.Println("✓ logged in as", username)

	// 2. Find the project to deploy into.
	projects, err := client.ListProjects(ctx)
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}
	var projectID string
	for _, p := range projects {
		if p.Name == projectName {
			projectID = p.ID
		}
	}
	if projectID == "" {
		return fmt.Errorf("no project named %q; have %d projects", projectName, len(projects))
	}
	fmt.Println("✓ deploying into project", projectName)

	// 3. Deploy the BPMN definition.
	defID, err := client.ImportDefinition(ctx, projectID, []byte(refundProcess))
	if err != nil {
		return fmt.Errorf("import definition: %w", err)
	}
	fmt.Println("✓ deployed definition", defID)

	// 4. Serve the external task from this process — this is what a payment
	// service integrating with gobpm would run.
	worker := gobpm.NewWorker(client, "reverse-charge", "quickstart-worker",
		gobpm.WorkerOptions{PollInterval: 300 * time.Millisecond},
		func(_ context.Context, task *gobpm.ExternalTask) (gobpm.Variables, error) {
			fmt.Printf("✓ worker got task %s with amount %v — reversing the charge\n",
				task.ID, task.Variables["amount"])
			return gobpm.Variables{"reversed": true}, nil
		})
	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	go func() { _ = worker.Run(workerCtx) }()

	// 5. Start an instance.
	instanceID, err := client.StartProcess(ctx, projectID, "refund", gobpm.Variables{"amount": 42.50})
	if err != nil {
		return fmt.Errorf("start process: %w", err)
	}
	fmt.Println("✓ started instance", instanceID)

	// 6. The worker completes the charge; a human task appears. Wait for it.
	task, err := waitForTask(ctx, client, instanceID)
	if err != nil {
		return err
	}
	fmt.Printf("✓ human task waiting: %q\n", task.Name)

	// 7. Claim and complete it, as a task-inbox application would.
	if err := client.ClaimTask(ctx, task.ID, username); err != nil {
		return fmt.Errorf("claim: %w", err)
	}
	if err := client.CompleteTask(ctx, task.ID, username, gobpm.Variables{"approved": true}); err != nil {
		return fmt.Errorf("complete: %w", err)
	}
	fmt.Println("✓ approved the refund")

	// 8. The instance should now be finished.
	if err := waitForStatus(ctx, client, instanceID, "completed"); err != nil {
		return err
	}
	fmt.Println("✓ instance completed")

	// 9. Read the story back.
	entries, err := client.GetTimeline(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("timeline: %w", err)
	}
	fmt.Printf("✓ timeline has %d entries:\n", len(entries))
	for _, e := range entries {
		line := cmp.Or(e.Narrative, e.Message)
		fmt.Println("   •", line)
	}
	return nil
}

// waitForTask polls until the instance's human task shows up in the inbox.
func waitForTask(ctx context.Context, client *gobpm.Client, instanceID string) (*gobpm.Task, error) {
	for {
		tasks, _, err := client.ListTasks(ctx, gobpm.ListTasksOptions{PageSize: 100})
		if err != nil {
			return nil, fmt.Errorf("list tasks: %w", err)
		}
		for i := range tasks {
			if tasks[i].Instance != nil && tasks[i].Instance.ID == instanceID {
				return &tasks[i], nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("no human task appeared for instance %s: %w", instanceID, ctx.Err())
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// waitForStatus polls until the instance reaches the wanted status.
func waitForStatus(ctx context.Context, client *gobpm.Client, instanceID, want string) error {
	for {
		instance, err := client.GetInstance(ctx, instanceID)
		if err != nil {
			return fmt.Errorf("get instance: %w", err)
		}
		if instance.Status == want {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("instance %s is %q, wanted %q: %w", instanceID, instance.Status, want, ctx.Err())
		case <-time.After(300 * time.Millisecond):
		}
	}
}
