/**
 * useAppStore — the application store.
 *
 * One store, persisted under one key, which `services/shared/auth.ts` reads
 * the token from outside React. Three "focused sub-stores" used to sit beside
 * it, each persisting to its own key; nothing imported them, and a token
 * written to one of theirs would have been invisible to every request.
 *
 * Subscribe with a selector — `useAppStore((state) => state.theme)` — rather
 * than destructuring the whole store, or the component re-renders on every
 * write to any field.
 */
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { renamedStorage } from './persistedStorage';

interface AppState {
  theme: 'light' | 'dark';
  toggleTheme: () => void;
  sidebarExpanded: boolean;
  toggleSidebar: () => void;
  currentProjectId: string | null;
  setCurrentProjectId: (id: string | null) => void;
  currentOrganizationId: string | null;
  setCurrentOrganizationId: (id: string | null) => void;
  activeTab: string;
  setActiveTab: (tab: string) => void;
  expertMode: boolean;
  setExpertMode: (val: boolean) => void;
  user: {
    id: string;
    name: string;
    displayName: string;
    organization: string;
    username: string;
    role: string;
    // Optional: a session saved before the store kept the list has only `role`.
    roles?: string[];
    organizations?: Array<{ id: string; name: string }>;
    projects?: Array<{ id: string; name: string }>;
  } | null;
  token: string | null;
  setAuth: (user: {
    id: string;
    name: string;
    displayName: string;
    organization: string;
    username: string;
    role: string;
    // Optional: a session saved before the store kept the list has only `role`.
    roles?: string[];
    organizations?: Array<{ id: string; name: string }>;
    projects?: Array<{ id: string; name: string }>;
  }, token: string) => void;
  clearAuth: () => void;
}

export const useAppStore = create<AppState>()(
  persist(
    (set) => ({
      theme: 'light',
      toggleTheme: () => set((state) => ({
        theme: state.theme === 'light' ? 'dark' : 'light',
      })),
      // Expanded by default: collapsed, the navigation is eleven unlabelled
      // icons, and the primary persona for this product is a non-technical
      // business user who has never seen them before. The collapse is still
      // there for people who know where things are and want the width back,
      // and the choice is remembered.
      sidebarExpanded: true,
      toggleSidebar: () => set((state) => ({
        sidebarExpanded: !state.sidebarExpanded,
      })),
      currentProjectId: null,
      setCurrentProjectId: (id) => set({ currentProjectId: id }),
      currentOrganizationId: null,
      setCurrentOrganizationId: (id) => set({ currentOrganizationId: id }),
      activeTab: 'dashboard',
      setActiveTab: (tab) => set({ activeTab: tab }),
      expertMode: false,
      setExpertMode: (val) => set({ expertMode: val }),
      user: null,
      token: null,
      setAuth: (user, token) => set({
        user,
        token,
        currentOrganizationId: user.organizations && user.organizations.length > 0 ? user.organizations[0].id : null,
        currentProjectId: user.projects && user.projects.length > 0 ? user.projects[0].id : null,
      }),
      clearAuth: () => set({
        user: null,
        token: null,
        activeTab: 'dashboard',
        currentOrganizationId: null,
        currentProjectId: null,
      }),
    }),
    {
      name: 'metis-app-storage',
      storage: renamedStorage('metis-app-storage'),
    },
  ),
);
