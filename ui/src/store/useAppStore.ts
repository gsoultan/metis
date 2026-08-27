/**
 * useAppStore — the main application store.
 *
 * FE-ARCH-2: Focused sub-stores (useUIStore, useAuthStore, useNavigationStore)
 * are available for new code that only needs a single slice.  This monolithic
 * store is kept as the primary store so that zustand's built-in getState()
 * works in route guards and other non-React contexts.
 */
import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

export { useUIStore } from './useUIStore';
export { useAuthStore } from './useAuthStore';
export type { AuthUser } from './useAuthStore';
export { useNavigationStore } from './useNavigationStore';

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
      name: 'gobpm-app-storage',
      storage: createJSONStorage(() => localStorage),
    },
  ),
);
