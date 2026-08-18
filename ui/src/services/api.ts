import {
  authService,
  collaborationService,
  connectorService,
  decisionService,
  definitionService,
  identityService,
  notificationService,
  organizationService,
  processRuntimeService,
  projectService,
  setupService,
  webhookService,
  signalService,
  taskService,
} from "./domains";

export type { Task, Project } from "./types";

/**
 * Facade over the domain services.
 *
 * The type is inferred. It was annotated `any`, which erased every type the
 * domain services define and left callers nothing to infer from — the single
 * largest source of untyped code in this app.
 */
export const processService = {
  ...authService,
  ...organizationService,
  ...projectService,
  ...processRuntimeService,
  ...taskService,
  ...definitionService,
  ...decisionService,
  ...signalService,
  ...connectorService,
  ...collaborationService,
  ...identityService,
  ...notificationService,
  ...setupService,
  ...webhookService,
};
