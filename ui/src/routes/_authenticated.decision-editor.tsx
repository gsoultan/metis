import { createFileRoute } from '@tanstack/react-router';
import { DecisionEditor } from '../pages/DecisionEditor';
import { z } from 'zod';

const searchSchema = z.object({
  id: z.string().optional(),
  name: z.string().optional(),
  key: z.string().optional(),
});

// Named, capitalised component rather than an inline arrow on `component:`.
// An anonymous lowercase function that calls hooks is invisible to the rules-of-hooks
// lint rule, so nothing would catch a conditional or nested hook call inside it.
// It also gives the component a name in React DevTools and lets Fast Refresh
// preserve state across edits.
function DecisionEditorRoute() {
  const { id } = Route.useSearch();
  return <DecisionEditor definitionId={id} />;
}

export const Route = createFileRoute('/_authenticated/decision-editor')({
  validateSearch: searchSchema,
  component: DecisionEditorRoute,
});
