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

export interface ApiConnector {
  /** Icon key used by the connector catalogue in the designer. */
  icon?: string;
  id: string;
  key: string;
  name: string;
  description?: string;
  category?: string;
  config_schema?: Record<string, unknown>;
}

export interface ApiConnectorInstance {
  id: string;
  connector_key: string;
  name: string;
  project_id: string;
  config?: Record<string, unknown>;
}

export interface CreateConnectorInstancePayload {
  connector_key: string;
  name: string;
  project_id: string;
  config?: Record<string, unknown>;
}

// ─── Process Runtime ─────────────────────────────────────────────────────────

export interface ApiAuditEntry {
  id: string;
  instance_id: string;
  node_id?: string;
  action: string;
  timestamp: string;
  details?: Record<string, unknown>;
}

export interface ApiSubProcess {
  id: string;
  parent_instance_id: string;
  status: string;
  active_nodes?: string[];
}

// ─── Identity ────────────────────────────────────────────────────────────────

export interface ApiOrganizationUser {
  id: string;
  username: string;
  fullName?: string;
  display_name?: string;
  organization?: string;
  email?: string;
  roles?: string[];
  organization_id?: string;
}

export interface ApiGroup {
  id: string;
  name: string;
  description?: string;
  organization_id?: string;
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
  values: ProcessVariables;
}
