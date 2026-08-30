/**
 * useUIStore — manages visual presentation state (theme, sidebar, expertMode).
 *
 * FE-ARCH-2: Separated from the monolithic useAppStore to satisfy SRP.
 * Persisted to localStorage under the key 'metis-ui-storage'.
 */
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { renamedStorage } from './persistedStorage';

interface UIState {
  theme: 'light' | 'dark';
  toggleTheme: () => void;
  sidebarExpanded: boolean;
  toggleSidebar: () => void;
  activeTab: string;
  setActiveTab: (tab: string) => void;
  expertMode: boolean;
  setExpertMode: (val: boolean) => void;
}

export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      theme: 'light',
      toggleTheme: () =>
        set((state) => ({
          theme: state.theme === 'light' ? 'dark' : 'light',
        })),
      sidebarExpanded: false,
      toggleSidebar: () =>
        set((state) => ({
          sidebarExpanded: !state.sidebarExpanded,
        })),
      activeTab: 'dashboard',
      setActiveTab: (tab) => set({ activeTab: tab }),
      expertMode: false,
      setExpertMode: (val) => set({ expertMode: val }),
    }),
    {
      name: 'metis-ui-storage',
      storage: renamedStorage('metis-ui-storage'),
    },
  ),
);

