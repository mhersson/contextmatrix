import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

// The markdown libraries render raw HTML live unless the sanitizer in
// src/components/Markdown runs last, so only that module may load them.
// `.` stands in for `/`, which esquery regexes cannot contain.
const markdownLibraries =
  '(@uiw.react-markdown-preview|@uiw.react-md-editor|react-markdown|rehype-raw|rehype-attr|rehype-ignore|rehype-sanitize|hast-util-raw|hast-util-sanitize)'
const markdownLibraryMessage =
  'Render markdown through src/components/Markdown (SafeMarkdownPreview / SafeMDEditor) so raw HTML stays sanitized.'
const nodeModulesMessage = 'Import packages by name; a node_modules path slips past the import guards.'
const innerHTMLMessage = 'Render HTML through src/components/Markdown; dangerouslySetInnerHTML bypasses the sanitizer.'

// Apply everywhere, the Markdown module included.
const guardedImportPatterns = [{ regex: '(^|/)node_modules/', message: nodeModulesMessage }]
const guardedSyntax = [
  {
    selector: "ImportExpression[source.type!='Literal']",
    message: 'Pass import() a string literal so the import guards can check it.',
  },
  { selector: 'ImportExpression[source.value=/(^|.)node_modules./]', message: nodeModulesMessage },
  { selector: "CallExpression[callee.name='require']", message: 'The frontend is ESM; use import.' },
  { selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']", message: innerHTMLMessage },
  { selector: "Property[key.name='dangerouslySetInnerHTML']", message: innerHTMLMessage },
  { selector: "Property[key.value='dangerouslySetInnerHTML']", message: innerHTMLMessage },
]

const markdownImportPatterns = [{ regex: `^${markdownLibraries}(/|$)`, message: markdownLibraryMessage }]
const markdownSyntax = [
  { selector: `ImportExpression[source.value=/^${markdownLibraries}(.|$)/]`, message: markdownLibraryMessage },
]

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
    },
  },
  {
    files: ['**/*.{js,jsx,mjs,cjs}'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      parserOptions: { ecmaFeatures: { jsx: true } },
      globals: globals.browser,
    },
  },
  {
    files: ['**/*.{js,jsx,mjs,cjs,ts,tsx}'],
    rules: {
      'no-restricted-imports': ['error', { patterns: [...guardedImportPatterns, ...markdownImportPatterns] }],
      'no-restricted-syntax': ['error', ...guardedSyntax, ...markdownSyntax],
    },
  },
  {
    files: ['src/components/Markdown/**'],
    rules: {
      'no-restricted-imports': ['error', { patterns: guardedImportPatterns }],
      'no-restricted-syntax': ['error', ...guardedSyntax],
    },
  },
])
