/**
 * Starting points offered in the script editor.
 *
 * Kept apart from the components that use them: a module exporting both a
 * component and a constant cannot be hot-reloaded, so editing a script template
 * would reload the designer and lose the diagram in progress.
 */
export const SCRIPT_TEMPLATES = [
  {
    name: "Set Variable",
    description: "Update a process variable",
    code: "setVar('status', 'approved');"
  },
  {
    name: "Conditional Logic",
    description: "If/Else block",
    code: "if (amount > 1000) {\n  setVar('isLargeOrder', true);\n} else {\n  setVar('isLargeOrder', false);\n}"
  },
  {
    name: "Math Calculation",
    description: "Perform arithmetic",
    code: "const total = amount * 1.1; // Add 10% tax\nsetVar('totalWithTax', total);"
  },
  {
    name: "String Manipulation",
    description: "Format strings",
    code: "const greeting = 'Hello, ' + (firstName || 'User');\nsetVar('fullGreeting', greeting);"
  },
  {
    name: "Date Formatting",
    description: "Current date/time",
    code: "const now = new Date().toISOString();\nsetVar('processedAt', now);"
  }
];
