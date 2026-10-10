import { AUTH_STORAGE_KEY } from "./config";

type StorageState = {
  state?: {
    token?: string;
    user?: { id?: string } | null;
  };
};

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

export const getAuthHeaders = (): Record<string, string> => {
  const token = getAuthToken();
  if (!token) {
    return {};
  }

  return { Authorization: `Bearer ${token}` };
};
