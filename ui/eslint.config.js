import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import jsxA11y from 'eslint-plugin-jsx-a11y'
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
      jsxA11y.flatConfigs.recommended,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    rules: {
      // Icon-only controls must carry an accessible name. There were 67 of
      // them with none: a screen reader announced "button" 67 times, with no
      // way to tell which one deletes a process definition. Mantine's
      // ActionIcon is not a native <button> to the linter, so it is named
      // explicitly here.
      'jsx-a11y/control-has-associated-label': ['error', {
        labelAttributes: ['aria-label', 'title'],
        controlComponents: ['ActionIcon'],
        // Without this the rule walks table markup and reports every <td>
        // that contains interactive content as an unlabelled control.
        ignoreElements: ['td', 'th', 'tr', 'tbody', 'thead', 'table', 'audio', 'canvas', 'embed', 'input', 'textarea', 'tfoot', 'video'],
        depth: 3,
      }],

      // autoFocus is correct inside a dialog — focus must move into it when it
      // opens, or a keyboard user is left behind on the page underneath. The
      // rule cannot tell a dialog from a page, so it is a warning here rather
      // than an error, and each use should be inside a Modal.
      'jsx-a11y/no-autofocus': ['warn', { ignoreNonDOM: true }],
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
