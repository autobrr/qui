/// <reference types="vite/client" />

// Extend ImportMeta to include 'glob' and 'env' for Vite
interface ImportMeta {
  readonly env: ImportMetaEnv;
  glob: (pattern: string, options?: { eager?: boolean; import?: string; query?: string }) => Record<string, unknown>;
}
