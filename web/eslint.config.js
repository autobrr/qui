import path from 'node:path'

import js from '@eslint/js'
import { plugin as shadcn } from '@shadcn/lint'
import stylistic from '@stylistic/eslint-plugin'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import { globalIgnores } from 'eslint/config'
import globals from 'globals'
import tseslint from 'typescript-eslint'

import { disableReason } from './scripts/disable-reason-rule.mjs'
import { effectHookShape } from './scripts/effect-hook-shape-rule.mjs'
import { entryFiles } from './scripts/entry-files-rule.mjs'
import { layerOrder } from './scripts/layer-order-rule.mjs'
import { noComments } from './scripts/no-comments-rule.mjs'
import { noDirectEffect } from './scripts/no-direct-effect-rule.mjs'
import { noNativeTitle } from './scripts/no-native-title-rule.mjs'

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
  regex: '(^|/)torrent-table/(useBulkActionWrappers|useCrossSeedOrchestration|useFilterLifecycle)$',
  message: 'The list controller owns this. Use useTorrentListController from "@/hooks/useTorrentListController" instead.',
}, copyGroupImportPattern]

const radixDialogImportPattern = {
  regex: '^@radix-ui/react-(alert-)?dialog$',
  allowTypeImports: true,
  message: 'Use @/components/ui/dialog or @/components/ui/alert-dialog.',
}

const bulkActionModuleMessage = 'The bulk action module in @/components/torrents/bulk-actions owns the Bulk actions. Send a pick with useBulkActions from "@/components/torrents/bulk-actions/bulk-actions-context" instead.'

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

const bulkActionRestriction = {
  object: 'api',
  property: 'bulkAction',
  message: bulkActionModuleMessage,
}

const tabTitleRestriction = {
  object: 'document',
  property: 'title',
  message: 'The tab title module in @/hooks/useTabTitle owns the browser tab title. Give the route its title in staticData, and pass the page detail or the speeds with usePageTitle from "@/hooks/usePageTitle" instead.',
}

const tabTitleModuleFiles = ['src/hooks/useTabTitle.ts', 'src/hooks/useTabTitle.test.tsx']

const gazelleSiteMessage = 'The backend owns the RED/OPS site table. Read the sites from api.getCrossSeedGazelleSites or the gazellePlan of a search.'
const gazelleSiteDomains = '/orpheus\\.network|opsfet\\.ch|flacsfor\\.me|redacted\\.sh/i'
const pageShellMessage = 'A page renders Page from @/components/layout/Page, which owns the padding, the title, the description, and the controls. A page with sections renders SectionedPage, which renders Page.'
const gazelleSiteSelectors = [
  { selector: 'Literal[regex.pattern=/orpheus|opsfet|flacsfor|redacted/i]', message: gazelleSiteMessage },
  { selector: `Literal[value=${gazelleSiteDomains}]`, message: gazelleSiteMessage },
  { selector: `TemplateElement[value.raw=${gazelleSiteDomains}]`, message: gazelleSiteMessage },
]

const settingsKitMessage = 'Settings builds on the kit in @/components/kit: Section for a card, Editor for a form in a dialog or a sheet, ConfirmDialog for a question before a step. Read src/components/kit/AGENTS.md.'

const settingsKitImportPatterns = [{
  regex: '(^|/)ui/(card|dialog|alert-dialog|sheet|drawer)$',
  allowTypeImports: true,
  message: settingsKitMessage,
}, {
  regex: '^vaul$',
  allowTypeImports: true,
  message: settingsKitMessage,
}, {
  regex: '(^|/)ui/command$',
  importNames: ['CommandDialog'],
  message: settingsKitMessage,
}]

const settingsFiles = [
  'src/pages/Settings.tsx',
  'src/components/settings/**',
  'src/components/instances/*.{ts,tsx}',
  'src/components/indexers/**',
  'src/components/display-profiles/**',
  'src/components/themes/**',
]

const settingsKitAllowlist = [
  'src/components/settings/ApiKeysManager.tsx',
  'src/components/settings/{RestartButton,InstallUpdateButton,RestartOverlay}.tsx',
  'src/components/display-profiles/{NewDisplayProfileDialog,DisplayProfileSheet}.tsx',
  'src/components/themes/CustomThemeEditor.tsx',
  'src/components/themes/ThemeMenuDialog.tsx',
]

const rawTextFieldSelectors = [{
  selector: [
    'JSXOpeningElement[name.name=/^(input|textarea)$/]',
    ':not(:has(JSXAttribute[name.name="type"] > Literal[value=/^(checkbox|radio|file|hidden|range|color)$/]))',
    ':not(:has(JSXAttribute[name.name="autoComplete"] > Literal[value=/(^|\\s)(username|current-password|new-password|one-time-code)(\\s|$)/]))',
    ':not(:has(JSXSpreadAttribute > CallExpression[callee.name="passwordManagerAttributes"]))',
  ].join(''),
  message: 'Use Input or Textarea from @/components/ui, which keep the password managers away. For a raw text field, spread passwordManagerAttributes() from @/lib/password-manager.',
}]

const tooltipDelaySelector = {
  selector: 'JSXAttribute[name.name="delayDuration"]',
  message: 'Every tooltip opens after the delay of TooltipProvider in @/components/ui/tooltip. Leave out delayDuration.',
}
const tooltipProviderSelector = {
  selector: 'JSXOpeningElement[name.name="TooltipProvider"]',
  message: 'App renders the one TooltipProvider. Leave out this provider, so that its tooltips open after the app-wide delay.',
}

const bottomBarMessage = 'Rows that the user picks end with SelectionBar from @/components/kit/SelectionBar, which stays at the bottom of the screen: docs/standards/frontend-review.md, "Selection". A bar at the bottom of the screen for another job goes in bottomBarAllowlist in web/eslint.config.js, with the reason as its value.'
const bottomBarPosition = '/(^|\\s)(sticky|fixed)(\\s|$)/'
const bottomBarEdge = '/(^|\\s)([\\w-]+:)*bottom-/'
const bottomBarSelectors = [
  { selector: `Literal[value=${bottomBarPosition}][value=${bottomBarEdge}]`, message: bottomBarMessage },
  { selector: `TemplateElement[value.raw=${bottomBarPosition}][value.raw=${bottomBarEdge}]`, message: bottomBarMessage },
]

const bottomBarAllowlist = {
  'src/components/kit/SelectionBar.tsx': 'The bar for the rows that the user picks.',
  'src/components/layout/MobileFooterNav.tsx': 'The nav at the bottom of a phone screen.',
  'src/components/torrents/TorrentCardsMobile.tsx': 'The torrent list on a phone has its own selection mode, over a list that loads its rows while the user scrolls.',
  'src/components/support/SupportDialog.tsx': 'On a phone, the checkout button of the support dialog stays in view.',
}

const clientSettingMessage = 'Read and write a UI preference with useClientSetting (or readClientSetting outside React) from "@/lib/client-settings". It maps the key to the active Display profile. A direct localStorage call skips that, so every profile shares the value.'

const clientSettingAllowlist = [
  'src/lib/client-settings.ts',
  'src/lib/theme-storage.ts',
  'src/hooks/useThemeSettingsSync.ts',
  'src/lib/protocol-handler.ts',
  'src/i18n/index.ts',
  'src/hooks/useClientSettingsSync.ts',
  'src/lib/dateTimeUtils.ts',
  'src/hooks/usePersistedDeleteFiles.ts',
  'src/hooks/usePersistedThemeVariation.ts',
  'src/components/problem-reports/draft.ts',
]

const layers = ['types', 'utils', 'config', 'i18n', 'lib', 'contexts', 'hooks', 'components', 'layouts', 'pages', 'routes']

const effectHooks = {
  'src/hooks/useDebounce.ts': 'Gives a value after it has stayed the same for a delay.',
  'src/hooks/useDelayedVisibility.ts': 'Says when the tab has been hidden for a delay.',
  'src/hooks/useNow.ts': 'Gives the time, and renders again each minute.',
  'src/hooks/usePageTitle.ts': 'Passes the title detail and the speeds of the open page to the tab title.',
  'src/hooks/useTabTitle.ts': 'Writes the browser tab title.',
}

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
      '@stylistic/quotes': ['error', 'double'],
      '@stylistic/comma-dangle': [
        'error',
        {
          arrays: 'always-multiline',
          objects: 'always-multiline',
          imports: 'never',
          exports: 'always-multiline',
          functions: 'never',
        },
      ],
      '@stylistic/indent': ['error', 2, { 'SwitchCase': 1 }],
      '@stylistic/no-trailing-spaces': ['error'],
      '@stylistic/object-curly-spacing': ['error', 'always'],
      '@typescript-eslint/no-unused-vars': ['error', { ignoreRestSiblings: true }],
      '@typescript-eslint/no-explicit-any': 'error',
      'linebreak-style': ['error', 'unix'],
      'react-refresh/only-export-components': ['error', { allowConstantExport: true }],
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'error',
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
    files: [...bulkActionModuleFiles, 'src/components/torrents/{TorrentDialogs,DeleteTorrentDialog}.test.tsx'],
    rules: {
      'no-restricted-imports': ['error', {
        patterns: [...torrentListImportPatterns, radixDialogImportPattern],
      }],
    },
  },
  {
    files: ['**/*.{ts,tsx}'],
    rules: {
      'no-restricted-properties': ['error', tabTitleRestriction],
    },
  },
  {
    files: ['**/*.{ts,tsx}'],
    ignores: [
      ...bulkActionModuleFiles,
      'src/components/torrents/TorrentDetailsPanel.tsx',
      'src/components/torrents/FilterSidebar.tsx',
      'src/lib/__tests__/api.torrents.test.ts',
      'src/lib/__tests__/api.mutations.test.ts',
      'src/components/torrents/table/__tests__/TorrentTableOptimized.smoke.test.tsx',
    ],
    rules: {
      'no-restricted-properties': ['error', bulkActionRestriction, tabTitleRestriction],
    },
  },
  {
    files: tabTitleModuleFiles,
    rules: {
      'no-restricted-properties': ['error', bulkActionRestriction],
    },
  },
  {
    files: ['src/hooks/useCopyGroupRows.ts'],
    rules: {
      'no-restricted-imports': ['error', {
        patterns: [...torrentListImportPatterns.filter(pattern => pattern !== copyGroupImportPattern), radixDialogImportPattern, bulkActionDialogImportPattern],
      }],
    },
  },
  {
    files: ['src/components/ui/{dialog,alert-dialog,sheet}.tsx'],
    rules: {
      'no-restricted-imports': ['error', { patterns: [...torrentListImportPatterns, bulkActionDialogImportPattern] }],
    },
  },
  {
    files: ['**/*.{ts,tsx}'],
    ignores: ['**/*.test.{ts,tsx}', '**/__tests__/**'],
    rules: {
      'no-restricted-syntax': ['error', ...gazelleSiteSelectors, ...rawTextFieldSelectors, ...bottomBarSelectors, tooltipDelaySelector, tooltipProviderSelector],
    },
  },
  {
    files: ['src/components/ui/**'],
    ignores: ['**/*.test.{ts,tsx}'],
    rules: {
      'no-restricted-syntax': ['error', ...gazelleSiteSelectors, tooltipDelaySelector, tooltipProviderSelector],
    },
  },
  {
    files: ['src/components/ui/tooltip.tsx'],
    rules: {
      'no-restricted-syntax': ['error', ...gazelleSiteSelectors],
    },
  },
  {
    files: ['src/App.tsx'],
    rules: {
      'no-restricted-syntax': ['error', ...gazelleSiteSelectors, ...rawTextFieldSelectors, ...bottomBarSelectors, tooltipDelaySelector],
    },
  },
  {
    files: ['src/components/layout/Sidebar.tsx'],
    rules: {
      'no-restricted-syntax': ['error', ...gazelleSiteSelectors, ...rawTextFieldSelectors, ...bottomBarSelectors, tooltipProviderSelector],
    },
  },
  {
    files: ['src/components/ui/SystemNoticeList.tsx'],
    rules: {
      'no-restricted-syntax': ['error', ...gazelleSiteSelectors, tooltipProviderSelector],
    },
  },
  {
    files: Object.keys(bottomBarAllowlist),
    rules: {
      'no-restricted-syntax': ['error', ...gazelleSiteSelectors, ...rawTextFieldSelectors, tooltipDelaySelector, tooltipProviderSelector],
    },
  },
  {
    files: settingsFiles,
    ignores: settingsKitAllowlist,
    rules: {
      'no-restricted-imports': ['error', {
        patterns: [...torrentListImportPatterns, radixDialogImportPattern, bulkActionDialogImportPattern, ...settingsKitImportPatterns],
      }],
    },
  },
  {
    files: ['src/pages/*.tsx'],
    ignores: ['**/*.test.tsx', 'src/pages/{Login,NotFound,Setup,Torrents}.tsx'],
    rules: {
      'no-restricted-syntax': ['error',
        ...gazelleSiteSelectors,
        ...rawTextFieldSelectors,
        ...bottomBarSelectors,
        tooltipDelaySelector,
        tooltipProviderSelector,
        { selector: 'Program:not(:has(ImportDeclaration[source.value=/^@.components.layout.(Page|SectionedPage)$/]))', message: pageShellMessage },
        { selector: 'Program:not(:has(JSXOpeningElement[name.name=/^(Page|SectionedPage)$/]))', message: pageShellMessage },
        { selector: 'JSXElement[openingElement.name.name=/^[a-z]/] JSXOpeningElement[name.name=/^(Page|SectionedPage)$/]', message: 'Render Page as the root of the page, with no HTML element around it.' },
      ],
    },
  },
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['**/*.test.{ts,tsx}', '**/__tests__/**', 'src/test/**', ...clientSettingAllowlist],
    rules: {
      'no-restricted-globals': ['error', {
        globals: [{ name: 'localStorage', message: clientSettingMessage }],
        checkGlobalObject: true,
      }],
      '@typescript-eslint/no-restricted-imports': ['error', {
        patterns: [{
          regex: '(^|/)client-settings$',
          importNames: ['readRaw', 'writeRaw'],
          message: clientSettingMessage,
        }],
      }],
    },
  },
  {
    files: ['scripts/**/*.mjs'],
    extends: [js.configs.recommended],
    languageOptions: {
      globals: globals.node,
    },
  },
  {
    plugins: { qui: { rules: { 'disable-reason': disableReason, 'effect-hook-shape': effectHookShape, 'entry-files': entryFiles, 'layer-order': layerOrder, 'no-comments': noComments, 'no-direct-effect': noDirectEffect, 'no-native-title': noNativeTitle } } },
    rules: {
      'qui/disable-reason': 'error',
      'qui/no-comments': 'error',
      'qui/no-native-title': 'error',
    },
  },
  {
    files: ['src/**/*.generated.ts'],
    rules: {
      'qui/no-comments': 'off',
    },
  },
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['**/*.test.{ts,tsx}', '**/__tests__/**', 'src/test/**', ...Object.keys(effectHooks)],
    rules: {
      'qui/no-direct-effect': 'error',
    },
  },
  {
    files: Object.keys(effectHooks),
    rules: {
      'qui/effect-hook-shape': 'error',
    },
  },
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['**/*.test.{ts,tsx}', '**/__tests__/**', 'src/test/**'],
    rules: {
      '@typescript-eslint/consistent-type-assertions': ['error', { assertionStyle: 'never' }],
      '@typescript-eslint/no-non-null-assertion': 'error',
      'qui/entry-files': ['error', { root: path.join(import.meta.dirname, 'src'), features: 'components' }],
      'qui/layer-order': ['error', { root: path.join(import.meta.dirname, 'src'), layers }],
    },
  },
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['**/*.test.{ts,tsx}', '**/__tests__/**', 'src/test/**'],
    plugins: { shadcn },
    settings: {
      shadcn: {
        note: 'Ask the maintainer before you add an option to a primitive.',
      },
    },
    rules: {
      'shadcn/no-unknown-classes': ['error', { allow: ['ss-*', 'fi', 'animated-logo-element', 'toaster'] }],
      'shadcn/require-static-classes': 'error',
    },
  },
  {
    files: ['src/components/{ui,kit}/**/*.{ts,tsx}'],
    rules: {
      'shadcn/require-static-classes': 'off',
    },
  },
])