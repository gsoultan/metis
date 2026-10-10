package impl

import "github.com/gsoultan/metis/server/repositories/models"

// The sentences a passed-over instance is told, asked of a version as the
// tests written before a run read its steps' names once ask them. A run reads
// the names once (stepsOfSource) and asks the sentences of that; these read
// them for the one sentence, and say what the run says.

func stepNames(source models.ProcessDefinitionModel, nodeIDs []string) string {
	return stepsOfSource(source).quoted(nodeIDs)
}

func leftTheStep(source models.ProcessDefinitionModel, nodeID string) string {
	return stepsOfSource(source).leftTheStep(nodeID)
}

func noLongerRunning(source models.ProcessDefinitionModel) string {
	return stepsOfSource(source).noLongerRunning()
}

func notPlannedFor(source models.ProcessDefinitionModel) string {
	return stepsOfSource(source).notPlannedFor()
}

func alreadyMoved(source models.ProcessDefinitionModel) string {
	return stepsOfSource(source).alreadyMoved()
}

func nowhereToLand(source, target models.ProcessDefinitionModel, nodeIDs []string) string {
	return stepsOfSource(source).nowhereToLand(target, nodeIDs)
}

func leftWhereNothingDecides(source, target models.ProcessDefinitionModel, nodeIDs []string) string {
	return stepsOfSource(source).leftWhereNothingDecides(target, nodeIDs)
}

func waitingToBeDecided(source models.ProcessDefinitionModel, nodeIDs []string) string {
	return stepsOfSource(source).waitingToBeDecided(nodeIDs)
}

func countersWouldMerge(source models.ProcessDefinitionModel) string {
	return stepsOfSource(source).countersWouldMerge()
}
