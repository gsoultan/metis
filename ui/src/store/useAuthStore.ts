/**
 * useAuthStore — manages authentication state (user, JWT token).
 *
 * FE-ARCH-2: Separated from the monolithic useAppStore to satisfy SRP.
 * Persisted to localStorage under the key 'metis-auth-storage'.
 */
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { renamedStorage } from './persistedStorage';

export interface AuthUser {
  id: string;
  name: string;
  username: string;
  role: string;
  organizations?: Array<{ id: string; name: string }>;
  projects?: Array<{ id: string; name: string }>;
}

interface AuthState {
  user: AuthUser | null;
  token: string | null;
  setAuth: (user: AuthUser, token: string) => void;
  clearAuth: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      user: null,
      token: null,
      setAuth: (user, token) => set({ user, token }),
      clearAuth: () => set({ user: null, token: null }),
    }),
    {
      name: 'metis-auth-storage',
      storage: renamedStorage('metis-auth-storage'),
    },
  ),
);

