import js from '@eslint/js'
import stylistic from '@stylistic/eslint-plugin'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import { globalIgnores } from 'eslint/config'
import globals from 'globals'
import tseslint from 'typescript-eslint'

const torrentListImportPatterns = [{
  regex: '(^|/)useTorrentsList$',
  allowTypeImports: true,
  message: 'Use useTorrentListController from "@/hooks/useTorrentListController" instead.',
}, {
  // The torrent-table hooks that moved into the controller. The hooks that stay outside it stay allowed.
  regex: '(^|/)torrent-table/(useBulkActionWrappers|useCrossSeedOrchestration|useFilterLifecycle)$',
  message: 'The list controller owns this. Use useTorrentListController or useTorrentListActions from "@/hooks/useTorrentListController" instead.',
}]

export default tseslint.config([
  globalIgnores(['dist', '@web/pnpm-lock.yaml', 'vite.config.ts']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    plugins: {
      '@stylistic': stylistic,
      'react-hooks': reactHooks,
    },
    rules: {
      '@stylistic/quotes': ['warn', 'double'],
      '@stylistic/comma-dangle': [
        'warn',
        {
          arrays: 'always-multiline',
          objects: 'always-multiline',
          imports: 'never',
          exports: 'always-multiline',
          functions: 'never',
        },
      ],
      '@stylistic/indent': ['error', 2, { 'SwitchCase': 1 }],
      '@stylistic/no-trailing-spaces': ['warn'],
      '@stylistic/object-curly-spacing': ['error', 'always'],
      '@typescript-eslint/no-unused-vars': ['warn', { ignoreRestSiblings: true }],
      '@typescript-eslint/no-explicit-any': 'error',
      'linebreak-style': ['error', 'unix'],
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',
    }
  },
  {
    files: ['**/*.{ts,tsx}'],
    ignores: [
      'src/hooks/useTorrentListController.ts',
      'src/hooks/useTorrentListController.test.tsx',
      'src/hooks/useTorrentsList.test.tsx',
    ],
    rules: {
      'no-restricted-imports': ['error', {
        patterns: [...torrentListImportPatterns, {
          regex: '^@radix-ui/react-(alert-)?dialog$',
          allowTypeImports: true,
          message: 'Use @/components/ui/dialog or @/components/ui/alert-dialog.',
        }],
      }],
    },
  },
  {
    // The shared dialog components wrap Radix, so only the Radix pattern is lifted here.
    files: ['src/components/ui/{dialog,alert-dialog,sheet}.tsx'],
    rules: {
      'no-restricted-imports': ['error', { patterns: torrentListImportPatterns }],
    },
  },
  {
    files: ['scripts/**/*.mjs'],
    extends: [js.configs.recommended],
    languageOptions: {
      globals: globals.node,
    },
  },
])