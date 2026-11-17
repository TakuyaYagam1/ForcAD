// Shared by handwritten code and the Orval post-generation formatter.
// The pinned ESLint 10 release still provides these formatting rules.
export const multilineRules = {
  curly: ['error', 'all'],
  'arrow-body-style': ['error', 'always'],
  'brace-style': ['error', '1tbs', {
    allowSingleLine: false
  }],
  'max-statements-per-line': ['error', {
    max: 1
  }],
  'object-property-newline': ['error', {
    allowAllPropertiesOnSameLine: false
  }],
  'object-curly-newline': ['error', {
    ObjectExpression: {
      minProperties: 1,
      multiline: true,
      consistent: true
    },
  }],
  'multiline/type-members': 'error',
  'no-trailing-spaces': 'error',
  indent: ['error', 2, {
    SwitchCase: 1
  }],
}
