/**
 * Who the suite signs in as, and where it keeps the signed-in browser state.
 *
 * The defaults match scripts/e2e.sh, which sets the sample up with these.
 * The people come from scripts/seed-sample.sh, which gives them all one password.
 */
export const ADMIN_USER = process.env.E2E_ADMIN_USER ?? 'admin';
export const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? 'e2e-admin-password';
export const SAMPLE_PASSWORD = process.env.E2E_SAMPLE_PASSWORD ?? 'sample-password';

export const ADMIN_STATE = '.auth/admin.json';

/** The localStorage key the UI keeps its session under (ui/src/services/shared/config.ts). */
export const AUTH_STORAGE_KEY = 'metis-app-storage';
