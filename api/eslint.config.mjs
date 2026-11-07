import { multilineRules } from '../frontend/eslint.multiline.js'
import { multilinePlugin } from '../frontend/eslint.multiline-types.js'

export default [{
  files: ['**/*.mjs'],
  plugins: { multiline: multilinePlugin },
  rules: multilineRules,
}]
