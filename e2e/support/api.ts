import { expect, type APIRequestContext } from '@playwright/test';

/**
 * Setting a test's scene up through the API, the way scripts/seed-sample.sh
 * does, so a spec about the inbox does not first have to drive the designer.
 */

/** The sample's one project. */
export async function sampleProjectId(api: APIRequestContext): Promise<string> {
  const response = await api.get('/api/v1/projects');
  expect(response.ok()).toBe(true);
  const { projects } = (await response.json()) as { projects: Array<{ id: string; name: string }> };
  const project = projects.find((p) => p.name === 'Sample Project') ?? projects[0];
  expect(project, 'the sample has a project').toBeTruthy();
  return project.id;
}

export interface FormField {
  id: string;
  label: string;
  type: 'boolean' | 'text' | 'textarea' | 'number';
  required?: boolean;
}

/**
 * Deploys a process of one person's task between a start and an end.
 *
 * The task names nobody, so it is an administrator's or an operator's to take
 * (decision D1) — which puts it in the admin's "Available to Claim".
 */
export async function deployReviewProcess(
  api: APIRequestContext,
  projectId: string,
  key: string,
  name: string,
  taskName: string,
  form: FormField[],
): Promise<void> {
  const definition = {
    key,
    name,
    project: { id: projectId },
    nodes: [
      { id: 'start', name: 'Request received', type: 'startEvent', x: 80, y: 200 },
      {
        id: 'review',
        name: taskName,
        type: 'userTask',
        x: 280,
        y: 190,
        properties: { form_definition: JSON.stringify(form) },
      },
      { id: 'end', name: 'Request handled', type: 'endEvent', x: 500, y: 200 },
    ],
    flows: [
      { id: 'f1', source_ref: 'start', target_ref: 'review' },
      { id: 'f2', source_ref: 'review', target_ref: 'end' },
    ],
  };
  const response = await api.post('/api/v1/definitions', { data: { definition } });
  expect(response.ok(), `deploying ${key}: ${await response.text()}`).toBe(true);
}

/** Starts an instance and returns its id. */
export async function startProcess(
  api: APIRequestContext,
  projectId: string,
  definitionKey: string,
  variables: Record<string, unknown> = {},
): Promise<string> {
  const response = await api.post('/api/v1/process.ProcessService/StartProcess', {
    data: { projectId, definitionKey, variables },
  });
  expect(response.ok(), `starting ${definitionKey}: ${await response.text()}`).toBe(true);
  const { instanceId } = (await response.json()) as { instanceId: string };
  return instanceId;
}

/** The short reference the UI shows for an instance: the last six characters of its id. */
export function shortReference(instanceId: string): string {
  return instanceId.replace(/-/g, '').slice(-6).toUpperCase();
}
