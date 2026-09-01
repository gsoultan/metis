/**
 * API_BASE_URL is relative by default.
 *
 * In production the Go server serves this bundle and the API from the same
 * origin, so a relative path is correct wherever it is deployed. It was
 * previously hardcoded to `http://localhost:8080/api/v1`, which meant any
 * deployment not running on the user's own machine shipped a UI that called
 * localhost from the visitor's browser.
 *
 * In development, vite.config.ts proxies /api to the backend, so the same
 * relative path works there too and no CORS is involved. Set
 * VITE_API_BASE_URL to point at a different backend (for example when running
 * the UI against a remote environment).
 */
export const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? "/api/v1";

export const AUTH_STORAGE_KEY = "metis-app-storage";
