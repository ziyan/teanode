import javascript from '@eslint/js'
import typescript from '@typescript-eslint/eslint-plugin'
import parser from '@typescript-eslint/parser'
import prettier from 'eslint-config-prettier'
import hooks from 'eslint-plugin-react-hooks'
import globals from 'globals'

export default [
  { ignores: ['node_modules/**', 'build/**', 'graphql/index.ts'] },
  {
    files: ['src/**/*.{ts,tsx}'],
    languageOptions: {
      parser,
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: globals.browser,
      parserOptions: { ecmaFeatures: { jsx: true } },
    },
    plugins: { '@typescript-eslint': typescript, 'react-hooks': hooks },
    rules: {
      ...javascript.configs.recommended.rules,
      ...typescript.configs['eslint-recommended'].overrides[0].rules,
      ...typescript.configs.recommended.rules,
      ...prettier.rules,
      '@typescript-eslint/no-explicit-any': 'off',
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
      '@typescript-eslint/no-empty-function': 'off',
      '@typescript-eslint/ban-ts-comment': 'off',
      'no-empty': 'off',
      'prefer-const': 'error',
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',
    },
  },
]
