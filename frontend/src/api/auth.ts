// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/**
 * Auth API client (§12).
 *
 *   POST /auth/login            { username, password } → { token, user }
 *   GET  /auth/me               current principal (behind middleware)
 *   POST /auth/change-password  { old_password, new_password }
 */
import { apiClient } from './client';
import type { AuthUser } from '@/stores/authStore';

export interface LoginResult {
  token: string;
  user: AuthUser;
}

export async function login(username: string, password: string): Promise<LoginResult> {
  const { data } = await apiClient.post<{ code: number; data: LoginResult }>('/auth/login', {
    username,
    password,
  });
  return data.data;
}

export async function changePassword(oldPassword: string, newPassword: string): Promise<void> {
  await apiClient.post('/auth/change-password', {
    old_password: oldPassword,
    new_password: newPassword,
  });
}
