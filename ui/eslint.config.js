import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

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
      // Keep the Mantine/Tailwind boundary from eroding.
      //
      // Mantine owns components, Tailwind owns layout. Restyling a Mantine
      // component with Tailwind colour/typography utilities produces
      // specificity fights and two sources of truth for the same decision —
      // which is the failure mode that makes "we use both" a net negative.
      // Layout utilities on Mantine components are fine; these are not.
      'no-restricted-syntax': ['error', {
        selector:
          "JSXElement[openingElement.name.name=/^(Button|Badge|Card|Paper|Table|Modal|Select|TextInput|ActionIcon|ThemeIcon|Alert|Menu)$/]" +
          " > JSXOpeningElement > JSXAttribute[name.name='className']" +
          "[value.value=/\\b(bg-|text-(?!left|right|center)|border-(?!0)|rounded|shadow|font-)/]",
        message:
          'Style Mantine components with Mantine props (c, bg, radius, fw), not Tailwind utilities. ' +
          'Tailwind is for layout: flex, grid, gap, responsive composition.',
      }],
      // An underscore prefix is the conventional way to say "deliberately
      // unused" — for a parameter required by a signature, or a destructured
      // field being omitted. Without this, the convention reads as an error.
      '@typescript-eslint/no-unused-vars': ['error', {
        argsIgnorePattern: '^_',
        varsIgnorePattern: '^_',
        caughtErrorsIgnorePattern: '^_',
        destructuredArrayIgnorePattern: '^_',
      }],
    },
  },
])
