import { Code, ConnectError, createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { DefinitionService } from "../../gen/services/definition_pb";
import { OrganizationService } from "../../gen/services/organization_pb";
import { ProcessService } from "../../gen/services/process_pb";
import { ProjectService } from "../../gen/services/project_pb";
import { SignalService } from "../../gen/services/signal_pb";
import { StatsService } from "../../gen/services/stats_pb";
import { TaskService } from "../../gen/services/task_pb";
import { UserService } from "../../gen/services/user_pb";
import { API_BASE_URL } from "./config";
import { getAuthToken } from "./auth";
import { reportSessionRefused } from "./sessionRefusal";

/**
 * Connect RPC transport for the first-party UI.
 *
 * The split is deliberate: this application talks to the server over Connect,
 * where the protobuf schema gives both ends a checked contract; third parties
 * integrate over the REST API, where a hand-written JSON shape is the friendlier
 * thing to consume.
 *
 * Upgraded to Connect v2 / protobuf-es v2. Two changes matter at call sites:
 *
 *   - `createPromiseClient` is now `createClient`. The old name distinguished
 *     it from a callback client that no longer exists.
 *   - Generated code emits *schemas* rather than message classes, so service
 *     descriptors live in `*_pb.ts` beside their messages and the separate
 *     `*_connect.ts` files are gone. Messages are plain objects; construct them
 *     with `create(SomeSchema, {...})` when a literal will not do.
 */
export const transport = createConnectTransport({
  baseUrl: API_BASE_URL,
  interceptors: [
    (next) => async (req) => {
      const token = getAuthToken();
      if (!token) {
        return next(req);
      }
      req.header.set("Authorization", `Bearer ${token}`);
      try {
        return await next(req);
      } catch (error) {
        // An expired or revoked token: the session is over, not this call.
        if (ConnectError.from(error).code === Code.Unauthenticated) {
          reportSessionRefused(token);
        }
        throw error;
      }
    },
  ],
});

export const organizationClient = createClient(OrganizationService, transport);
export const projectClient = createClient(ProjectService, transport);
export const processClient = createClient(ProcessService, transport);
export const taskClient = createClient(TaskService, transport);
export const definitionClient = createClient(DefinitionService, transport);
export const signalClient = createClient(SignalService, transport);
export const statsClient = createClient(StatsService, transport);
export const userClient = createClient(UserService, transport);
