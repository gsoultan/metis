/**
 * Typed API contracts for every service call.
 *
 * FE-ARCH-11: replaces `any` and `unknown` response shapes throughout the
 * service and hook layers.  JSON REST responses are typed here; protobuf
 * RPC responses use the generated types from src/gen.
 *
 * Generated proto types are re-exported for convenience so callers only need
 * to import from this module.
 */

import type { JsonObject } from "@bufbuild/protobuf";
import type { Project } from "../gen/entities/project_pb";
import type { Task } from "../gen/entities/task_pb";

export type { Project, Task };

// ─── Auth ────────────────────────────────────────────────────────────────────

/** Shape of the user object returned by /login. */
export interface ApiUser {
  id: string;
  name: string;
  username: string;
  /**
   * The server populates this from entities.User.Roles, so it arrives as an
   * array despite the singular name. Declared as both because older responses
   * and some list endpoints send a bare string.
   */
  role: string | string[];
  organizations?: Array<{ id: string; name: string }>;
  projects?: Array<{ id: string; name: string }>;
}

/** Shape of the /login REST response. */
export interface LoginResponse {
  user?: ApiUser;
  token?: string;
}

// ─── Definitions ─────────────────────────────────────────────────────────────

/** Extended definition node as returned by the REST API (richer than proto). */
export interface ApiNode {
  id: string;
  name: string;
  type: string;
  x: number;
  y: number;
  /** Size and expansion from an imported diagram; absent for anything drawn here. */
  width?: number;
  height?: number;
  is_expanded?: boolean;
  assignee?: string;
  candidate_users?: Array<{ username: string; full_name?: string; display_name?: string }>;
  candidate_groups?: Array<{ name: string }>;
  priority?: number;
  due_date?: string;
  form_key?: string;
  default_flow?: string;
  script?: string;
  script_format?: string;
  external_topic?: string;
  documentation?: string;
  attached_to_ref?: string;
  parent_id?: string;
  cancel_activity?: boolean;
  /** Error boundary events match on this; empty catches every error. */
  error_code?: string;
  multi_instance_type?: string;
  loop_cardinality?: number;
  collection?: string;
  element_variable?: string;
  completion_condition?: string;
  is_event_sub_process?: boolean;
  condition?: string;
  properties?: Record<string, unknown>;
}

/** Sequence flow as returned by the REST API. */
export interface ApiFlow {
  id: string;
  source_ref: string;
  target_ref: string;
  condition?: string;
  documentation?: string;
  /** The route the edge was drawn along, when it came from an imported file. */
  waypoints?: ApiWaypoint[];
}

/** One point on a sequence flow's drawn route. */
export interface ApiWaypoint {
  x: number;
  y: number;
}

/** Full definition with nodes and flows as returned by getDefinition. */
export interface ApiDefinition {
  id: string;
  project_id: string;
  key: string;
  name: string;
  version: number;
  nodes: ApiNode[];
  flows: ApiFlow[];
}

export interface ExportDefinitionResponse {
  xml?: string;
  err?: string;
}

export interface ImportDefinitionResponse {
  definition?: ApiDefinition;
  err?: string;
}

// ─── Process request (for createDefinition) ───────────────────────────────────

/** Node payload sent to createDefinition. */
export interface CreateNodePayload {
  id: string;
  name: string;
  type: string | undefined;
  x: number;
  y: number;
  width: number;
  height: number;
  is_expanded: boolean;
  assignee: string;
  candidate_users: string[];
  candidate_groups: string[];
  priority: number;
  due_date: string;
  form_key: string;
  default_flow: string;
  script: string;
  script_format: string;
  external_topic: string;
  documentation: string;
  attached_to_ref: string;
  parent_id: string;
  cancel_activity: boolean;
  error_code: string;
  multi_instance_type: string;
  loop_cardinality: number;
  collection: string;
  element_variable: string;
  completion_condition: string;
  is_event_sub_process: boolean;
  condition: string;
  properties: Record<string, unknown>;
}

/** Flow payload sent to createDefinition. */
export interface CreateFlowPayload {
  id: string;
  source_ref: string;
  target_ref: string;
  condition: string;
  documentation: string;
  waypoints: ApiWaypoint[];
}

/** Request body for createDefinition. */
export interface CreateDefinitionPayload {
  key: string;
  name: string;
  nodes: CreateNodePayload[];
  flows: CreateFlowPayload[];
}

/**
 * One deployed version of a process, with what it is still carrying.
 *
 * `running_instances` is the part that matters when replacing a version.
 * Instances never move between versions — each one finishes on the graph it
 * started on — so a version that has stopped being live keeps executing until
 * this reaches zero. That is what `draining` names.
 */
export interface ApiDefinitionVersion {
  id: string;
  key: string;
  name: string;
  version: number;
  created_at?: string;
  /** The single version new instances start on. */
  live: boolean;
  running_instances: number;
  total_instances: number;
  /**
   * When this version is arranged to take over, if a cutover naming it is still
   * in the future. Absent when none is.
   *
   * Nothing runs at that moment: the server resolves the live version from the
   * release timeline and the clock on every read, so the cutover happens by the
   * time arriving.
   */
  scheduled_for?: string;
  /** The timeline entry behind `scheduled_for`, which is what cancelling names. */
  scheduled_release_id?: string;
}

export interface ListDefinitionVersionsResponse {
  versions?: ApiDefinitionVersion[];
  err?: string;
}

export interface PromoteDefinitionResponse {
  err?: string;
}

export interface ScheduleDefinitionResponse {
  err?: string;
}

export interface CancelScheduledDefinitionResponse {
  err?: string;
}

/** One node's worth of a migration plan: where work sits, and where it lands. */
export interface ApiNodeMove {
  from: string;
  to: string;
  tokens: number;
  tasks: number;
  jobs: number;
  /**
   * The subset of `tasks` somebody is holding right now.
   *
   * Counted apart because they cost differently: an unclaimed task is a queue
   * item nobody has started, a claimed one is a person with the form open who
   * is about to lose their place, because a task that changes node has its
   * assignment re-derived from the node it lands on.
   */
  tasks_claimed?: number;
  tasks_delegated?: number;
  /**
   * Message or signal subscriptions waiting on this node.
   *
   * Counted apart from jobs because a subscription is a promise to somebody
   * outside the process: a timer that does not fire is a delay, a message that
   * correlates to nothing is a caller who never gets an answer.
   */
  events?: number;
  /** False when the node keeps its id and is carried over without a mapping. */
  mapped: boolean;
}

/** What a migration does with the work on one node instead of moving it. */
export type NodeActionKind = 'skip' | 'cancel' | 'hold';

/** One node whose work a migration decides rather than moves. */
export interface ApiNodeAction {
  kind: NodeActionKind;
  /** Why. Required: without it the trail cannot tell a skipped step from a performed one. */
  reason: string;
}

/** One decided node, as the plan reports it back. */
export interface ApiPlannedNodeAction {
  node_id: string;
  name?: string;
  kind: NodeActionKind;
  reason?: string;
}

/** One control-bearing step a migration would drop. */
export interface ApiComplianceHold {
  node_id: string;
  name?: string;
  /** Why the step is there, as the modeller wrote it. */
  note?: string;
  /** How many running instances have not passed it yet. */
  instances: number;
}

/** What moving running instances onto another version would do. */
export interface ApiMigrationPlan {
  source_key: string;
  source_version: number;
  target_version: number;
  target_id: string;
  instances: number;
  moves?: ApiNodeMove[];
  /** Non-empty means the apply would be refused, and why. */
  refusals?: string[];
  /**
   * What to look at before applying, as distinct from what would be refused.
   *
   * Kept apart from `refusals` on purpose: a warning that reads like a refusal
   * teaches people to click past the list, and then a real refusal gets clicked
   * past too.
   */
  warnings?: string[];
  /**
   * Nodes whose work this migration decides rather than moves.
   *
   * Reported back so a preview shows the decisions as prominently as the moves:
   * "two instances move" and "two instances have an approval skipped" must not
   * read identically.
   */
  actions?: ApiPlannedNodeAction[];
  /**
   * Control-bearing steps this migration would take away from instances that
   * have not performed them yet.
   *
   * A hold is a refusal the operator may accept by name. It is not a warning:
   * accepting one is the act that turns "the approval was skipped" into "the
   * approval was skipped, by this person, who was told what it was for".
   */
  compliance_holds?: ApiComplianceHold[];
  /**
   * The nodes the new version no longer has, whether or not work sits on one.
   *
   * A removed user task is also the removed producer of everything its form
   * used to write, and the gateways downstream still read those variables.
   */
  removed_nodes?: string[];
}

export interface MigrateInstancesResponse {
  plan: ApiMigrationPlan;
  applied?: boolean;
  err?: string;
}

/**
 * Which version of each process key new instances start on.
 *
 * A key that nobody has promoted is absent rather than zero, and the reader
 * resolves it the way the engine does: the highest version deployed.
 */
/**
 * One runtime a project deploys into, and the database it owns.
 *
 * `connection.password` is never the real one: the API returns a sentinel, and
 * sending it back means "keep what is stored". See internal/pkg/configsecret.
 */
export interface ApiEnvironment {
  id: string;
  project?: { id: string };
  name: string;
  port: number;
  driver: string;
  connection?: Record<string, unknown>;
  enabled: boolean;
  created_at?: string;
}

export interface ListEnvironmentsResponse {
  environments?: ApiEnvironment[];
  err?: string;
}

export interface SaveEnvironmentResponse {
  id?: string;
  err?: string;
}

export interface TestEnvironmentConnectionResponse {
  reachable?: boolean;
  detail?: string;
  err?: string;
}

export interface DeleteEnvironmentResponse {
  err?: string;
}

export interface ListLiveVersionsResponse {
  live?: Record<string, number>;
  err?: string;
}

// ─── Connectors ──────────────────────────────────────────────────────────────

/**
 * One field a connector needs configuring: a webhook URL, an API key, a host.
 * The catalogue ships these so the UI can build a form without knowing what
 * any particular connector is.
 */
export interface ApiConnectorProperty {
  key: string;
  label: string;
  /** string | password | boolean | number | select | textarea, and mapping for a step field */
  type: string;
  description?: string;
  default_value?: string;
  required?: boolean;
  /** Choices, when type is select. */
  options?: unknown[];
}

/**
 * A connector in the built-in catalogue.
 *
 * These names are the server's. They previously said `category` and
 * `config_schema`, which the API has never sent — it sends `type` and
 * `schema` — so every read of them was undefined and every page that touched
 * a connector had to fall back to `any` to compile.
 */
export interface ApiConnector {
  id: string;
  key: string;
  name: string;
  description?: string;
  /** Lucide icon name, used by the catalogue and the designer. */
  icon?: string;
  /** e.g. communication, utility */
  type?: string;
  schema?: ApiConnectorProperty[];
  /**
   * What a step using this connector fills in — a database lookup's query,
   * its values, and where the answer goes. Absent for a connector a step only
   * names.
   */
  node_schema?: ApiConnectorProperty[];
  created_at?: string;
}

/**
 * A connector configured for one project — a specific Slack workspace rather
 * than "Slack".
 *
 * The project and the connector are nested objects, not ids. Describing them
 * as `project_id` and `connector_key` is what let the connector page send ids
 * the API ignored, so every instance it created was stored belonging to no
 * project and configuring no connector.
 */
export interface ApiConnectorInstance {
  id: string;
  name: string;
  project?: { id: string; name?: string };
  connector?: { id: string; key?: string; name?: string; type?: string };
  config?: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
}

/** What createConnector sends: the server assigns the id. */
export type CreateConnectorPayload = Omit<ApiConnector, 'id' | 'created_at'>;

/** What createConnectorInstance sends. Same shape, minus the server's fields. */
export interface CreateConnectorInstancePayload {
  id?: string;
  name: string;
  project: { id: string };
  connector: { id: string };
  config?: Record<string, unknown>;
}

// ─── Process Runtime ─────────────────────────────────────────────────────────

/**
 * One line of an instance's history, as /instances/{id}/audit returns it.
 *
 * These names are the server's. The type previously described an `action` and
 * `details` with flat ids, none of which are sent — the timeline reads type,
 * message, narrative, node and data, and was right to.
 */
export interface ApiAuditEntry {
  id: string;
  /** e.g. process_started, node_reached, variable_updated */
  type: string;
  message: string;
  /** A sentence written for a person rather than an operator. */
  narrative?: string;
  timestamp: string;
  node?: { id: string; name?: string; type?: string };
  instance?: { id: string };
  project?: { id: string };
  data?: Record<string, unknown>;
}

/**
 * A sub-process instance, as /instances/{id}/subprocesses returns one.
 *
 * The parent instance and the call activity that started it are nested
 * objects. Describing them as `parent_instance_id` meant the call activity
 * panel matched on a field that is never sent, so it never found the running
 * sub-process and never offered to open it.
 */
export interface ApiSubProcess {
  id: string;
  status: string;
  parent_instance?: { id: string };
  parent_node?: { id: string };
  definition?: { id: string; key?: string; name?: string };
  variables?: Record<string, unknown>;
  created_at?: string;
}

// ─── Identity ────────────────────────────────────────────────────────────────

/**
 * A user as the REST API sends one.
 *
 * The name field is `full_name`. It was declared `fullName` here, which the API
 * has never sent, so every read of it was undefined: names showed as usernames
 * in the member and assignee lists, searching by name matched nothing, and —
 * the expensive one — the edit form opened with an empty Full Name and saving
 * submitted that empty value back.
 */
export interface ApiOrganizationUser {
  id: string;
  username: string;
  full_name?: string;
  display_name?: string;
  /** The API sends the organization as an object, not a name. */
  organization?: { id: string; name?: string };
  email?: string;
  roles?: string[];
  organizations?: Array<{ id: string; name?: string }>;
}

/**
 * A group of users, which a user task can assign work to by name.
 *
 * `roles` is read by the group editor and was missing here, and the API sends
 * the owning organization as an object rather than an `organization_id`.
 */
export interface ApiGroup {
  id: string;
  name: string;
  description?: string;
  roles?: string[];
  organization?: { id: string; name?: string };
  created_at?: string;
}

export interface CreateUserPayload {
  organization_id: string;
  username: string;
  password: string;
  full_name: string;
  display_name: string;
  organization: string;
  email: string;
  roles: string[];
}

// ─── Setup ───────────────────────────────────────────────────────────────────

export interface SetupRequest {
  database_driver: string;
  db_host: string;
  db_port: number;
  db_username: string;
  db_password: string;
  db_name: string;
  db_ssl_enabled: boolean;
  encryption_key: string;
  jwt_secret: string;
  admin_username: string;
  admin_password: string;
  admin_full_name: string;
  admin_public_name: string;
  admin_email: string;
  organization_name: string;
  project_name: string;
}


/**
 * ProcessVariables is the business payload carried by a process instance, task
 * or decision evaluation. The keys and value shapes are defined by whoever
 * modelled the process, so they are not knowable at compile time.
 *
 * Aliased to protobuf's JsonObject rather than Record<string, unknown>: these
 * cross the wire inside a protobuf Struct, which can only hold JSON, and
 * protobuf-es v2 enforces that. Saying so here means a value that could not
 * survive the round trip is rejected where it is built rather than at the
 * transport boundary.
 */
export type ProcessVariables = JsonObject;

/** Mirrors entities.DecisionInput. */
export interface ApiDecisionInput {
  id: string;
  label: string;
  expression: string;
  /** "string" | "number" | "boolean" */
  type: string;
}

/** Mirrors entities.DecisionOutput. */
export interface ApiDecisionOutput {
  id: string;
  label: string;
  name: string;
  type: string;
  /**
   * The allowed result values, most important first. PRIORITY and OUTPUT ORDER
   * rank matches by this list, and refuse to evaluate without it.
   */
  values?: string[];
}

/**
 * Mirrors entities.DecisionRule. Rule outputs are authored per decision table,
 * so their types vary by column.
 */
export interface ApiDecisionRule {
  id: string;
  inputs?: string[];
  outputs?: unknown[];
  description?: string;
}

/** Mirrors entities.DecisionDefinition. */
export interface ApiDecision {
  id: string;
  project?: { id: string };
  key: string;
  name: string;
  version: number;
  hit_policy: string;
  aggregation?: string;
  required_decisions?: string[];
  inputs?: ApiDecisionInput[];
  outputs?: ApiDecisionOutput[];
  rules?: ApiDecisionRule[];
  /** Examples this table is expected to get right. */
  tests?: Array<{
    id: string;
    name: string;
    inputs?: Record<string, unknown>;
    expected?: Record<string, unknown>;
  }>;
  created_at?: string;
}

/**
 * Mirrors entities.DecisionSummary: one decision key, without the table — what
 * the decision list, the dependency graph and a step's picker need. `id`,
 * `name`, `version`, `hit_policy` and `required_decisions` are the live
 * version's, or the newest's when none is live.
 */
export interface ApiDecisionSummary {
  id: string;
  key: string;
  name: string;
  version: number;
  hit_policy?: string;
  required_decisions?: string[];
  /** The version in force; 0 when none is. */
  live_version?: number;
  /** The highest stored version. Above live_version, it is staged. */
  newest_version?: number;
  /** When a version was last saved or made live. */
  last_changed_at?: string;
}

/** Payload accepted when creating or updating a decision. */
export type CreateDecisionPayload = Omit<ApiDecision, 'id' | 'version' | 'created_at'> &
  Partial<Pick<ApiDecision, 'id' | 'version'>>;

/** Mirrors entities.DecisionResult. */
export interface DecisionResult {
  /**
   * Positions in the table of the lines that produced this result, in table
   * order — one under FIRST, every match under COLLECT. The editor highlights
   * them, so an evaluation shows its reasoning and not only its answer.
   */
  matched_rules?: number[];
  values: ProcessVariables;
}

/** One run of a connector step against its project's saved connection. */
export interface TryConnectorStepRequest {
  projectId: string;
  stepName?: string;
  /** The step's settings, under the names the server stores them by. */
  properties: Record<string, unknown>;
  variables: Record<string, unknown>;
}
