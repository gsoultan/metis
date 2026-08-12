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
  signalService,
  taskService,
} from "./domains";

export type { Task, Project } from "./types";

/**
 * Facade over the domain services.
 *
 * TODO(P0.6): remove the `any` and let the type be inferred.
 *
 * This annotation erases every type the domain services already define and is
 * the single largest source of untyped code in the app — most remaining
 * `no-explicit-any` violations exist because callers of this object have
 * nothing to infer from.
 *
 * Removing it was attempted and reverted: it surfaces 19 cascading type errors
 * across 10 files, which need real fixes rather than a mechanical sweep. It
 * also immediately exposed four genuine defects, all of which ARE fixed here:
 *
 *   - SetupStatus was declared as a string union no endpoint returns, while
 *     three route guards read `status.is_initialized` off it;
 *   - the server sends `role` as an array, the store declared a string;
 *   - the store requires `displayName` and `organization`, which the login
 *     endpoint has never returned (see mappers/userMapper.ts);
 *   - `processService.listDeployments` is called by useDeployments but no
 *     domain service defines it — it is `undefined` at runtime.
 *
 * That hit rate is the argument for finishing the job.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const processService: any = {
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
};
