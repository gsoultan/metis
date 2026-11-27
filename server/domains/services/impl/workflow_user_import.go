package impl

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic/userimport"
	"github.com/gsoultan/metis/server/domains/services/impl/participantsource"
)

// maxCheckedRows bounds the rows a check lists. The counts cover every row;
// the list is for reading, and past a few thousand nobody reads it.
const maxCheckedRows = 2000

// csvSource reads an uploaded file, so a file is a source like the others.
type csvSource struct {
	body    []byte
	mapping userimport.Mapping
}

func (c csvSource) Fetch(context.Context) (userimport.Result, error) {
	return userimport.ParseWith(bytes.NewReader(c.body), c.mapping)
}

// sourceFor builds the reader a request describes.
//
// A database source needs a caller allowed to write queries: an
// administrator, or somebody holding the Query author role. The import is open
// to designers, and the other two sources read only what a file or a URL
// returns; this one runs the caller's SQL against a database the caller names,
// which is the power a database lookup keeps behind that role. A check asks
// the same, because it runs the same query.
func sourceFor(ctx context.Context, req entities.DirectoryImport) (participantsource.Source, error) {
	mapping := userimport.Mapping(req.Mapping)
	if err := mapping.Validate(); err != nil {
		return nil, apierr.Invalidf("%s", err.Error())
	}
	switch req.Kind {
	case "csv", "":
		if len(req.CSV) == 0 {
			return nil, apierr.Invalidf("no file was uploaded")
		}
		return csvSource{body: req.CSV, mapping: mapping}, nil
	case string(participantsource.KindHTTP):
		return participantsource.NewHTTPSource(participantsource.HTTPConfig{
			URL: req.URL, Method: req.Method, Headers: req.Headers, Mapping: mapping,
		}), nil
	case string(participantsource.KindPostgres):
		if !mayAuthorQueries(ctx) {
			return nil, apierr.Forbiddenf("importing people with a database query needs the " +
				"Query author role; ask an administrator to grant it, or to run the import")
		}
		return participantsource.NewPostgresSource(participantsource.PostgresConfig{
			DSN: req.DSN, Query: req.Query, Mapping: mapping,
		}), nil
	default:
		return nil, apierr.Invalidf("%q is not a directory source; use csv, http or postgres", req.Kind)
	}
}

// fetch reads a request's source. A source that cannot be read at all is a
// refusal, not a list of problems: nothing was read, so nothing lands.
func fetch(ctx context.Context, projectID uuid.UUID, req entities.DirectoryImport) (userimport.Result, error) {
	if projectID == uuid.Nil {
		return userimport.Result{}, apierr.Invalidf("a project is required")
	}
	source, err := sourceFor(ctx, req)
	if err != nil {
		return userimport.Result{}, err
	}
	parsed, err := source.Fetch(ctx)
	if err != nil {
		return userimport.Result{}, apierr.Invalidf("%s", err.Error())
	}
	return parsed, nil
}

// ImportDirectory reads a directory into a project.
//
// Deliberately not one transaction. A file is a list of independent facts about
// independent people, and rolling the whole import back because row four
// hundred had a malformed address would discard three hundred and ninety-nine
// rows that were fine. What is atomic is each person: a rename and the tasks
// it moves commit together.
func (s *workflowUserService) ImportDirectory(ctx context.Context, projectID uuid.UUID, req entities.DirectoryImport) (entities.ImportSummary, error) {
	parsed, err := fetch(ctx, projectID, req)
	if err != nil {
		return entities.ImportSummary{}, err
	}
	return s.write(ctx, projectID, parsed, req.MoveOpenTasks)
}

// CheckDirectoryImport works out what ImportDirectory would do with the same
// request, and writes nothing.
//
// The same fetch and the same plan an import makes, so what a check reports is
// what an import does — unless the source or the directory changes in
// between, which is why the import reports its own counts again.
func (s *workflowUserService) CheckDirectoryImport(ctx context.Context, projectID uuid.UUID, req entities.DirectoryImport) (entities.ImportCheck, error) {
	parsed, err := fetch(ctx, projectID, req)
	if err != nil {
		return entities.ImportCheck{}, err
	}
	existing, err := s.participants.ListByProject(ctx, projectID, 0)
	if err != nil {
		return entities.ImportCheck{}, err
	}

	check := entities.ImportCheck{
		Read:   len(parsed.Rows) + len(parsed.Problems),
		Counts: map[string]int{},
		Fields: parsed.Fields,
		Sample: parsed.Sample,
		Rows:   []entities.CheckedRow{},
	}
	if check.Fields == nil {
		check.Fields = []string{}
	}
	var rows []entities.CheckedRow
	for _, problem := range parsed.Problems {
		check.Counts["invalid"]++
		rows = append(rows, entities.CheckedRow{Line: problem.Line, Outcome: "invalid", Username: problem.Username, Detail: problem.Reason})
	}
	for _, plan := range userimport.Plan(existing, parsed.Rows) {
		check.Counts[string(plan.Outcome)]++
		if plan.Outcome == userimport.OutcomeUnchanged {
			continue
		}
		row := entities.CheckedRow{
			Line:        plan.Row.Line,
			Outcome:     string(plan.Outcome),
			Username:    plan.Row.Username,
			ExternalID:  plan.Row.ExternalID,
			DisplayName: plan.Row.DisplayName,
			Email:       plan.Row.Email,
			Detail:      detailOf(plan),
		}
		if plan.Outcome == userimport.OutcomeRenamed {
			if row.OpenTasks, err = s.participants.CountOpenTasksNaming(ctx, projectID, plan.Person.Username); err != nil {
				return entities.ImportCheck{}, err
			}
		}
		rows = append(rows, row)
	}
	sortCheckedRows(rows)
	if len(rows) > maxCheckedRows {
		rows, check.Truncated = rows[:maxCheckedRows], true
	}
	check.Rows = append(check.Rows, rows...)
	return check, nil
}

// write applies what a source produced, row by row.
//
// An import is additive: somebody absent from the source is left alone, not
// deactivated. A partial file is far more common than a complete one, and
// taking people's work away because a file did not mention them is a change
// nobody asked for and nobody would see.
func (s *workflowUserService) write(ctx context.Context, projectID uuid.UUID, parsed userimport.Result, moveOpenTasks bool) (entities.ImportSummary, error) {
	summary := entities.ImportSummary{Problems: problemsFrom(parsed.Problems)}
	existing, err := s.participants.ListByProject(ctx, projectID, 0)
	if err != nil {
		return summary, err
	}
	// Groups already ensured in this source, so a directory where four
	// hundred people are in "approvers" looks the group up once.
	groups := map[string]uuid.UUID{}

	refuse := func(row userimport.Row, reason string) {
		summary.Problems = append(summary.Problems, entities.ImportProblem{Line: row.Line, Username: row.Username, Reason: reason})
	}

	for _, plan := range userimport.Plan(existing, parsed.Rows) {
		row := plan.Row
		switch plan.Outcome {
		case userimport.OutcomeConflict:
			refuse(row, plan.Reason)
			continue
		case userimport.OutcomeRenamed:
			moved, err := s.rename(ctx, projectID, plan, moveOpenTasks)
			if err != nil {
				refuse(row, err.Error())
				continue
			}
			summary.Renamed++
			summary.TasksMoved += moved
		}

		created, err := s.participants.Upsert(ctx, entities.WorkflowUser{
			Project:     &entities.Project{ID: projectID},
			Username:    row.Username,
			ExternalID:  row.ExternalID,
			DisplayName: row.DisplayName,
			Email:       row.Email,
			Active:      row.Active,
		})
		if err != nil {
			// One person failing is that row's problem, reported where
			// somebody can find it. The source carries on.
			refuse(row, reasonFor(err))
			continue
		}
		switch {
		case created:
			summary.Created++
		case plan.Outcome == userimport.OutcomeUnchanged:
			summary.Unchanged++
		case plan.Outcome == userimport.OutcomeUpdate, plan.Outcome == userimport.OutcomeLinked:
			summary.Updated++
		}

		if err := s.joinGroups(ctx, projectID, row, groups, &summary); err != nil {
			refuse(row, reasonFor(err))
		}
	}
	sortProblems(summary.Problems)
	return summary, nil
}

// rename applies a rename the source reported. One that would leave open
// tasks addressed to the old username is applied only when the caller said to
// move them: otherwise that work would sit in nobody's inbox.
func (s *workflowUserService) rename(ctx context.Context, projectID uuid.UUID, plan userimport.Planned, moveOpenTasks bool) (int, error) {
	from := plan.Person.Username
	open, err := s.participants.CountOpenTasksNaming(ctx, projectID, from)
	if err != nil {
		return 0, err
	}
	if open > 0 && !moveOpenTasks {
		return 0, fmt.Errorf("renamed from %q in the source, but %d open task(s) name %q; import again with their tasks moved to rename them", from, open, from)
	}
	return s.participants.Rename(ctx, projectID, plan.Person.ID, plan.Row.Username, open > 0)
}

// detailOf says, in words, what importing a planned row would do.
func detailOf(plan userimport.Planned) string {
	if plan.Outcome == userimport.OutcomeConflict {
		return plan.Reason
	}
	detail := ""
	for i, change := range plan.Changes {
		if i > 0 {
			detail += "; "
		}
		detail += change
	}
	return detail
}

func sortCheckedRows(rows []entities.CheckedRow) {
	slices.SortStableFunc(rows, func(a, b entities.CheckedRow) int { return a.Line - b.Line })
}

func sortProblems(problems []entities.ImportProblem) {
	slices.SortStableFunc(problems, func(a, b entities.ImportProblem) int { return a.Line - b.Line })
}
