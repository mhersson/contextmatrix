import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

// The markdown libraries let a body author inject HTML and attributes unless
// their unsafe rehype plugins are filtered out, so only the wrappers in
// src/components/Markdown may load them. `.` stands in for `/`, which esquery
// regexes cannot contain.
const markdownLibraries =
  '(@uiw.react-markdown-preview|@uiw.react-md-editor|react-markdown|rehype-raw|rehype-attr|rehype-ignore)'
const markdownLibraryMessage =
  'Render markdown through src/components/Markdown (SafeMarkdownPreview / SafeMDEditor) so raw HTML stays inert.'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    rules: {
      '@typescript-eslint/no-unused-vars': [
        'error',
        { argsIgnorePattern: '^_', varsIgnorePattern: '^_', caughtErrorsIgnorePattern: '^_' },
      ],
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            {
              regex: `^${markdownLibraries}(/|$)`,
              message: markdownLibraryMessage,
            },
          ],
        },
      ],
      'no-restricted-syntax': [
        'error',
        {
          selector: `ImportExpression[source.value=/^${markdownLibraries}(.|$)/]`,
          message: markdownLibraryMessage,
        },
        {
          selector: `ImportExpression[source.quasis.0.value.raw=/^${markdownLibraries}(.|$)/]`,
          message: markdownLibraryMessage,
        },
      ],
    },
  },
  {
    files: ['src/components/Markdown/**'],
    rules: {
      'no-restricted-imports': 'off',
      'no-restricted-syntax': 'off',
    },
  },
])
