import { AUTH_STORAGE_KEY } from "./config";

type StorageState = {
  state?: {
    token?: string;
    user?: { id?: string } | null;
    currentOrganizationId?: string | null;
  };
};

/**
 * The header that says which of the caller's organizations a request is for.
 *
 * The server validates it against the caller's memberships and, without it,
 * works in the first one. The app never sent it, so choosing another
 * organization in the header changed what the page said and nothing the server
 * did: every read and write still went to the first.
 */
export const ORGANIZATION_HEADER = "X-Organization-ID";

const parseStorageState = (): StorageState | null => {
  const storage = localStorage.getItem(AUTH_STORAGE_KEY);
  if (!storage) {
    return null;
  }

  try {
    return JSON.parse(storage) as StorageState;
  } catch (error) {
    console.error("Error parsing auth storage", error);
    return null;
  }
};

export const getAuthToken = (): string | null => {
  const state = parseStorageState();
  if (!state?.state?.token) {
    return null;
  }

  return state.state.token;
};

/** The signed-in person's id, or null when nobody is signed in. */
export const getAuthUserId = (): string | null => {
  const state = parseStorageState();
  if (!state?.state?.token) {
    return null;
  }

  return state.state.user?.id || null;
};

/** The organization being worked in, or null when none is chosen or nobody is signed in. */
export const getActiveOrganizationId = (): string | null => {
  const state = parseStorageState();
  if (!state?.state?.token) {
    return null;
  }

  return state.state.currentOrganizationId || null;
};

/** The headers a signed-in request carries: the token, and the organization it is for. */
export const getAuthHeaders = (): Record<string, string> => {
  const token = getAuthToken();
  if (!token) {
    return {};
  }

  const organization = getActiveOrganizationId();
  return {
    Authorization: `Bearer ${token}`,
    ...(organization ? { [ORGANIZATION_HEADER]: organization } : {}),
  };
};
