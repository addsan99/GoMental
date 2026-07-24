/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_TRANSPORT?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
