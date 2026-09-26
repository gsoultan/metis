package app

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/tests/testutils"
)

// A bridge publishes to one project's broker, so what it reads must be that
// organization's work and no other's. Started as system work — what the job
// worker and the timers run as — it would have forwarded every organization's
// tasks of its topic to that one broker.
func TestABridgeReadsOnlyItsOwnOrganizationsTasks(t *testing.T) {
	a := realRabbitMQApp(t)
	ours := seedRabbitMQConnection(t, a, "amqp://metis:s3cret-broker-password@broker.internal:5672/orders")
	theirsCtx, _, theirProject := testutils.ScopedProject(t, a.repo)
	oursCtx := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: ours.organization.String()})

	const topic = "reverse-charge"
	ourInstance := deployAndStart(t, oursCtx, a, externalTaskProcess(ours.project, topic), nil)
	deployAndStart(t, theirsCtx, a, externalTaskProcess(theirProject, topic), nil)

	runner := newRabbitMQRunner(a.svc, a.svc, a.projectOrganization)
	scoped, _, err := runner.resolve(t.Context(), rabbitMQTarget{project: ours.project, connection: ours.connection})
	if err != nil {
		t.Fatalf("resolve the bridge's connection: %v", err)
	}

	tasks, err := a.svc.FetchAndLock(scoped, topic, "messaging-bridge", 10, 30_000)
	if err != nil {
		t.Fatalf("fetch as the bridge does: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ProcessInstance == nil || tasks[0].ProcessInstance.ID != ourInstance {
		var instances []uuid.UUID
		for _, task := range tasks {
			instances = append(instances, task.ProcessInstance.ID)
		}
		t.Fatalf("the bridge read the tasks of instances %v; want only %s, its own organization's", instances, ourInstance)
	}
}

// externalTaskProcess is start → a step an outside worker performs → end.
func externalTaskProcess(project uuid.UUID, topic string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: project},
		Key:     "charge-" + topic,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "charge", Type: entities.ServiceTask, Name: "Charge the card", ExternalTopic: topic},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "charge"},
			{ID: "f2", SourceRef: "charge", TargetRef: "end"},
		},
	}
}
