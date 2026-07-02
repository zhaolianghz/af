// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_BASE_URL?: string;
  readonly VITE_API_DEBUG?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
