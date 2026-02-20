import js from '@eslint/js'
import stylistic from '@stylistic/eslint-plugin'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import { globalIgnores } from 'eslint/config'
import globals from 'globals'
import tseslint from 'typescript-eslint'

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
      '@stylistic/multiline-ternary': ['warn', 'never'],
      '@stylistic/no-trailing-spaces': ['warn'],
      '@stylistic/object-curly-spacing': ['error', 'always'],
      '@typescript-eslint/no-unused-vars': ['warn'],
      'linebreak-style': ['error', 'unix'],
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',
    }
  },
])