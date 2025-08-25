import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { globalIgnores } from 'eslint/config'
import stylistic from '@stylistic/eslint-plugin'

export default tseslint.config([
  globalIgnores(['dist', '@web/pnpm-lock.yaml']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs['recommended-latest'],
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    plugins: {
      '@stylistic': stylistic
    },
    rules: {
      '@stylistic/quotes': ['warn', 'double'],
      '@stylistic/comma-dangle': ['warn', 'always'],
      '@stylistic/multiline-ternary': ['warn', 'never'],
      '@stylistic/object-curly-spacing': ['error', 'always'],
      '@stylistic/indent': ['error', 2, { 'SwitchCase': 1 }],
      '@typescript-eslint/no-unused-vars': ['warn'],
      'linebreak-style': ['error', 'unix'],
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }]
    }
  },
])
