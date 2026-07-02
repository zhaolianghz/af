// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Auth store + token persistence for §12.
 *
 * The token lives in localStorage so a page reload keeps the session.
 * The store mirrors it in memory for synchronous reads (the axios
 * request interceptor and the Header both read it). When auth is
 * DISABLED on the backend (dev default), no token is ever set and the
 * app runs fully open — the 401 interceptor in client.ts only fires
 * when the backend actually enforces auth.
 */
import { create } from 'zustand';

const TOKEN_KEY = 'af_token';
const USER_KEY = 'af_user';

// localStorage is absent in non-jsdom test environments (and SSR). All
// access goes through these guards so importing this module never throws
// at init time when `localStorage` is undefined.
function lsGet(key: string): string | null {
  try {
    return typeof localStorage !== 'undefined' ? localStorage.getItem(key) : null;
  } catch {
    return null;
  }
}

function lsSet(key: string, value: string): void {
  try {
    if (typeof localStorage !== 'undefined') localStorage.setItem(key, value);
  } catch {
    /* ignore */
  }
}

function lsRemove(key: string): void {
  try {
    if (typeof localStorage !== 'undefined') localStorage.removeItem(key);
  } catch {
    /* ignore */
  }
}

export interface AuthUser {
  id: number;
  username: string;
  role_id: number;
}

function loadUser(): AuthUser | null {
  const raw = lsGet(USER_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as AuthUser;
  } catch {
    return null;
  }
}

interface AuthState {
  token: string | null;
  user: AuthUser | null;
  setSession: (token: string, user: AuthUser) => void;
  clear: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  token: lsGet(TOKEN_KEY),
  user: loadUser(),
  setSession: (token, user) => {
    lsSet(TOKEN_KEY, token);
    lsSet(USER_KEY, JSON.stringify(user));
    set({ token, user });
  },
  clear: () => {
    lsRemove(TOKEN_KEY);
    lsRemove(USER_KEY);
    set({ token: null, user: null });
  },
}));

// Non-React accessors for the axios interceptors (which run outside the
// React tree). They read/write the same localStorage + store.
export function getToken(): string | null {
  return lsGet(TOKEN_KEY);
}

export function clearSession(): void {
  useAuthStore.getState().clear();
}
