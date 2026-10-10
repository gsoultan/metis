package impl

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// passedOver is the run's account of an instance it left alone: which one,
// why as a code, the steps that is about and how many they were, and why in
// words. It is the one place such an account is made, and it is made from a
// reason one of the eight constructors made (migration_left_alone.go): so no
// account carries a cause outside the closed set, or none.
func passedOver(instance models.ProcessInstanceModel, why leftAlone) entities.PassedOverInstance {
	return entities.PassedOverInstance{
		Instance:   &entities.ProcessInstance{ID: uuid.UUID(instance.ID)},
		Cause:      why.cause,
		Steps:      why.steps,
		StepsInAll: why.stepsInAll,
		Reason:     why.reason,
	}
}

// sourceSteps is the version a run moves instances off, as the reasons for
// leaving one alone name it: its number, and what each of its steps is
// called.
//
// It is read once, for the run. A reason names steps, and a run over a
// thousand instances can give a thousand reasons; each used to look through
// the whole definition for every step it named, and again for the list of
// steps beside the sentence.
type sourceSteps struct {
	version int
	// names is the name the version gives each step, by id: nothing for a
	// step it gives no name.
	names map[string]string
}

// stepsOfSource reads the steps of a version, those inside its sub-processes
// too.
//
// A definition is somebody's input. This goes through the steps as they are
// written, each once, keeping its place in a list of its own rather than in
// a call inside a call: however deep the sub-processes are written, and
// whatever a step names as its parent — itself, or a step that names it —
// it reads each step and ends.
//
// Of two steps written under one id it keeps the first it meets, a
// sub-process before the steps inside it: the one the sentence named when it
// looked through the definition itself (nodeNameIn).
func stepsOfSource(source models.ProcessDefinitionModel) sourceSteps {
	names := map[string]string{}
	unread := [][]models.FlowNode{source.Nodes}
	for len(unread) > 0 {
		last := len(unread) - 1
		if len(unread[last]) == 0 {
			unread = unread[:last]
			continue
		}
		node := unread[last][0]
		unread[last] = unread[last][1:]
		if _, met := names[node.ID]; !met {
			names[node.ID] = node.Name
		}
		if len(node.Nodes) > 0 {
			unread = append(unread, node.Nodes)
		}
	}
	return sourceSteps{version: source.Version, names: names}
}

// name is what a step is called: the name the version gives it, and its id
// for a step with no name or one the version does not have.
func (s sourceSteps) name(nodeID string) string {
	if name := s.names[nodeID]; name != "" {
		return name
	}
	return nodeID
}

// leftTheStep is why a skip, a cancel or a hold did nothing to an instance: it
// found the instance, once locked, no longer on the step. The step is named as
// the source version names it, for the person reading the reply.
func (s sourceSteps) leftTheStep(nodeID string) string {
	return fmt.Sprintf("It was no longer waiting at %q when the migration reached it, so nothing was decided there "+
		"and it was not moved. It stays on version %d; if it is still running, run the same migration again "+
		"to plan for where it now stands.", s.name(nodeID), s.version)
}

// noLongerRunning is why an instance was not moved: it finished, or was ended,
// between the run listing it and locking it.
func (s sourceSteps) noLongerRunning() string {
	return fmt.Sprintf("It was no longer running when the migration reached it, so it was not moved. "+
		"It stays on version %d, the one it ran on.", s.version)
}

// notPlannedFor is why an instance was not moved: it was not on the source
// version when the migration was planned, so nothing the plan establishes was
// established for it. It started there afterwards, or another migration moved
// it there.
func (s sourceSteps) notPlannedFor() string {
	return fmt.Sprintf("It was not on version %d when this migration was planned: it started, or was moved there, "+
		"after that. Nothing had been asked about it, so nothing was decided about it and it was not moved. "+
		"It stays on version %d; plan the migration again to include it.", s.version, s.version)
}

// alreadyMoved is why an instance was not moved: once locked, it was no longer
// on the source version. Only a migration changes an instance's version, so
// another run — an earlier one whose listing this run shared, or one running
// at the same time — had moved it.
func (s sourceSteps) alreadyMoved() string {
	return fmt.Sprintf("It was no longer on version %d when the migration reached it: another run of a migration "+
		"had already moved it. Nothing was decided about it and it was not moved again.", s.version)
}

// nowhereToLand is why an instance was not moved: once locked, it held work —
// a token, a task, a timer, a waiting event or a counter — on steps the new
// version has no step for and the mapping does not cover. The planner refuses
// a migration for exactly this; the instance got there after the plan was
// made, by itself or because a skip advanced it there.
func (s sourceSteps) nowhereToLand(target models.ProcessDefinitionModel, nodeIDs []string) string {
	return fmt.Sprintf("When the migration came to move it, it had work at %s, and version %d has nowhere to put "+
		"that, so it was not moved. It stays on version %d. Plan the migration again for where it now stands: "+
		"it needs a mapping, or a decision, for that work.",
		s.quoted(nodeIDs), target.Version, s.version)
}

// leftWhereNothingDecides is why an instance was not moved: once locked, it
// had an open task or a waiting event on a step this migration decides, and no
// token there. A decision acts on the instances waiting at its step, so
// nothing would have settled that work, and the new version has no such step.
func (s sourceSteps) leftWhereNothingDecides(target models.ProcessDefinitionModel, nodeIDs []string) string {
	return fmt.Sprintf("When the migration came to move it, it had a task or a waiting event at %s, where this "+
		"migration decides the work of the instances waiting there, and it was not waiting there, so no decision "+
		"reached that work and version %d has nowhere to put it. It was not moved and stays on version %d. "+
		"Nothing in the product withdraws a single task or waiting event yet, so it stays there until that work is gone.",
		s.quoted(nodeIDs), target.Version, s.version)
}

// waitingToBeDecided is why an instance was not moved: once locked, it had a
// token on a step this migration decides rather than moves, and no decision had
// settled it. It reached the step after its work was decided, or a skip left
// part of the step behind.
func (s sourceSteps) waitingToBeDecided(nodeIDs []string) string {
	return fmt.Sprintf("When the migration came to move it, it was waiting at %s, where this migration decides the "+
		"work rather than moving it, and no decision had settled it, so it was not moved. It stays on version %d; "+
		"run the same migration again to decide it where it now stands.",
		s.quoted(nodeIDs), s.version)
}

// countersWouldMerge is why an instance was not moved: once locked, it held
// progress counters on two steps the mapping puts onto one.
func (s sourceSteps) countersWouldMerge() string {
	return fmt.Sprintf("When the migration came to move it, it was part-way through two steps that this mapping "+
		"moves onto one, and their progress cannot be added together, so it was not moved. It stays on version %d. "+
		"Plan the migration again for where it now stands: it needs those steps mapped apart.", s.version)
}

// quoted names steps as the source version names them, quoted, for a
// sentence: "A", "A" and "B", or "A", "B" and "C".
//
// It is bounded as the list beside the sentence is (stepsOf): the first
// entities.MaxPassedOverSteps steps, each name kept to the length a step's
// name is kept to wherever one is shown (shownStepName), and then how many
// were left out — "A", "B" and 2 more. An instance can hold work on as many
// steps as its version has, and a run gives a sentence for every instance it
// passes over. Up to ten steps with names of an ordinary length, it reads as
// it always did.
func (s sourceSteps) quoted(nodeIDs []string) string {
	shown := nodeIDs[:min(len(nodeIDs), entities.MaxPassedOverSteps)]
	names := make([]string, 0, len(shown))
	for _, id := range shown {
		names = append(names, fmt.Sprintf("%q", shownStepName(s.name(id))))
	}
	if more := len(nodeIDs) - len(shown); more > 0 {
		return fmt.Sprintf("%s and %d more", strings.Join(names, ", "), more)
	}
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
