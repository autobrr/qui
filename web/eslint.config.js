import js from '@eslint/js'
import stylistic from '@stylistic/eslint-plugin'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import { globalIgnores } from 'eslint/config'
import globals from 'globals'
import tseslint from 'typescript-eslint'

const copyGroupImportPattern = {
  regex: '(^|/)copy-groups$',
  importNames: ['summarizeCopyGroups', 'countTickedCopies'],
  message: 'Both Layouts read a Copy group through one hook. Use useCopyGroupRows from "@/hooks/useCopyGroupRows" instead.',
}

const torrentListImportPatterns = [{
  regex: '(^|/)useTorrentsList$',
  allowTypeImports: true,
  message: 'Use useTorrentListController from "@/hooks/useTorrentListController" instead.',
}, {
  // The torrent-table hooks that moved into the controller. The hooks that stay outside it stay allowed.
  regex: '(^|/)torrent-table/(useBulkActionWrappers|useCrossSeedOrchestration|useFilterLifecycle)$',
  message: 'The list controller owns this. Use useTorrentListController from "@/hooks/useTorrentListController" instead.',
}, copyGroupImportPattern]

const radixDialogImportPattern = {
  regex: '^@radix-ui/react-(alert-)?dialog$',
  allowTypeImports: true,
  message: 'Use @/components/ui/dialog or @/components/ui/alert-dialog.',
}

const bulkActionModuleMessage = 'The bulk action module in @/components/torrents/bulk-actions owns the Bulk actions. Send a pick with useBulkActions from "@/components/torrents/bulk-actions/bulk-actions-context" instead.'

// Rename file, Rename folder, and the edit tracker dialog stay out: the details panel and the filter sidebar use them.
const bulkActionDialogImportPattern = {
  regex: '(^|/)(TorrentDialogs|DeleteTorrentDialog)$',
  importNames: [
    'DeleteTorrentDialog',
    'TagEditorDialog',
    'SetCategoryDialog',
    'CreateAndAssignCategoryDialog',
    'SetLocationDialog',
    'LocationWarningDialog',
    'ShareLimitDialog',
    'SpeedLimitsDialog',
    'SetCommentDialog',
    'TmmConfirmDialog',
    'RenameTorrentDialog',
  ],
  message: bulkActionModuleMessage,
}

const bulkActionModuleFiles = ['src/components/torrents/bulk-actions/**']

// "<redacted>" and "gazelle.orpheusApiKey" are valid strings, so the string rule matches the full domains only.
const gazelleSiteMessage = 'The backend owns the RED/OPS site table. Read the sites from api.getCrossSeedGazelleSites or the gazellePlan of a search.'
const gazelleSiteDomains = '/orpheus\\.network|opsfet\\.ch|flacsfor\\.me|redacted\\.sh/i'

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
        patterns: [...torrentListImportPatterns, radixDialogImportPattern, bulkActionDialogImportPattern],
      }],
    },
  },
  {
    // The block above skips these files, so they get only the dialog pattern.
    files: [
      'src/hooks/useTorrentListController.ts',
      'src/hooks/useTorrentListController.test.tsx',
      'src/hooks/useTorrentsList.test.tsx',
    ],
    rules: {
      'no-restricted-imports': ['error', { patterns: [bulkActionDialogImportPattern] }],
    },
  },
  {
    // The bulk action module renders the dialogs, and each dialog test renders its dialog.
    files: [...bulkActionModuleFiles, 'src/components/torrents/{TorrentDialogs,DeleteTorrentDialog}.test.tsx'],
    rules: {
      'no-restricted-imports': ['error', {
        patterns: [...torrentListImportPatterns, radixDialogImportPattern],
      }],
    },
  },
  {
    files: ['**/*.{ts,tsx}'],
    ignores: [
      ...bulkActionModuleFiles,
      // The details panel and the filter sidebar act on one torrent or one tracker, not on the selection.
      'src/components/torrents/TorrentDetailsPanel.tsx',
      'src/components/torrents/FilterSidebar.tsx',
      // The API tests cover the request itself.
      'src/lib/__tests__/api.torrents.test.ts',
      'src/lib/__tests__/api.mutations.test.ts',
      // The table smoke test reads the requests that the module sends.
      'src/components/torrents/table/__tests__/TorrentTableOptimized.smoke.test.tsx',
    ],
    rules: {
      'no-restricted-properties': ['error', {
        object: 'api',
        property: 'bulkAction',
        message: bulkActionModuleMessage,
      }],
    },
  },
  {
    // The shared hook is the one reader of the Copy group summaries, so only that pattern is lifted here.
    files: ['src/hooks/useCopyGroupRows.ts'],
    rules: {
      'no-restricted-imports': ['error', {
        patterns: [...torrentListImportPatterns.filter(pattern => pattern !== copyGroupImportPattern), radixDialogImportPattern, bulkActionDialogImportPattern],
      }],
    },
  },
  {
    // The shared dialog components wrap Radix, so only the Radix pattern is lifted here.
    files: ['src/components/ui/{dialog,alert-dialog,sheet}.tsx'],
    rules: {
      'no-restricted-imports': ['error', { patterns: [...torrentListImportPatterns, bulkActionDialogImportPattern] }],
    },
  },
  {
    files: ['**/*.{ts,tsx}'],
    ignores: ['**/*.test.{ts,tsx}', '**/__tests__/**'],
    rules: {
      'no-restricted-syntax': ['error',
        { selector: 'Literal[regex.pattern=/orpheus|opsfet|flacsfor|redacted/i]', message: gazelleSiteMessage },
        { selector: `Literal[value=${gazelleSiteDomains}]`, message: gazelleSiteMessage },
        { selector: `TemplateElement[value.raw=${gazelleSiteDomains}]`, message: gazelleSiteMessage },
      ],
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