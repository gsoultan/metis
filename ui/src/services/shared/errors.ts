/**
 * Helpers for turning an unknown thrown value into something a person can act
 * on.
 *
 * `catch` bindings are `unknown`, not `Error`, so every call site was either
 * typing them `any` or discarding them. Discarding is the worse of the two: a
 * notification reading "Failed to save organization" tells the user nothing
 * about whether they should retry, fix a field, or call an administrator.
 */

/** Extracts a human-readable message from an unknown thrown value. */
export function errorMessage(error: unknown, fallback = 'Something went wrong'): string {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  if (typeof error === 'string' && error) {
    return error;
  }
  if (error && typeof error === 'object' && 'message' in error) {
    const { message } = error as { message?: unknown };
    if (typeof message === 'string' && message) {
      return message;
    }
  }
  return fallback;
}

/**
 * Builds the body of a failure notification: what was being attempted, then
 * why it failed.
 */
export function failureMessage(action: string, error: unknown): string {
  return `${action}: ${errorMessage(error)}`;
}
