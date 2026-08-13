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
}

/** Request body for createDefinition. */
export interface CreateDefinitionPayload {
  key: string;
  name: string;
  nodes: CreateNodePayload[];
  flows: CreateFlowPayload[];
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
  /** string | password | boolean | number | select */
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
  created_at?: string;
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
