/**
 * Turning a refusal the server reported into a failure the UI can see.
 *
 * Both transports in this app answer a write with the error in the body rather
 * than by failing the call: Connect RPC responses carry an `error` field, and
 * the HTTP endpoints carry `err`. That is a reasonable wire format — go-kit
 * endpoints deliberately reserve the transport error for transport failures —
 * but it means a refused write arrives as a *resolved* promise.
 *
 * Every service method wrapped it back up as `{ err }` and every mutation
 * ignored it. So claiming a task somebody else already had showed a green
 * "Task Claimed", and the list then refetched and showed it still assigned to
 * them. The same was true of every write in the app: 28 methods, one shape,
 * always reporting success.
 *
 * TanStack Query decides success or failure by whether the mutation function
 * rejects, so that is what has to happen. Every `onError` handler already
 * written then starts doing its job.
 */

/** As much of a reply as this needs to look at. */
interface MaybeFailed {
  err?: string | null;
  error?: string | null;
}

/**
 * Raises if the reply carries a refusal, and otherwise hands it back unchanged.
 *
 * Returns the reply so it can wrap a call in place:
 *
 *     return raiseIfRefused(await taskClient.claimTask({ id, userId }))
 */
export function raiseIfRefused<T extends MaybeFailed>(reply: T): T {
  const refusal = reply?.err ?? reply?.error;
  if (refusal) {
    throw new Error(refusal);
  }
  return reply;
}
