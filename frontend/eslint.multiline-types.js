// ESLint core does not visit TypeScript interface/type members for layout.
export const multilinePlugin = {
  rules: {
    'type-members': {
      meta: {
        type: 'layout',
        fixable: 'whitespace',
        schema: [],
        messages: {
          newline: 'Put type members on separate lines with consistent indentation.',
        },
      },
      create(context) {
        const source = context.sourceCode

        function check(node) {
          const members = node.type === 'TSTypeLiteral' ? node.members : node.body
          if (!members.length) {
            return
          }
          const open = source.getFirstToken(node)
          const close = source.getLastToken(node)
          const indent = source.lines[open.loc.start.line - 1].match(/^\s*/)[0]
          const newline = source.text.includes('\r\n') ? '\r\n' : '\n'
          const boundaries = [
            [open, members[0], indent + '  '],
            ...members.slice(1).map((member, index) => {
              return [members[index], member, indent + '  ']
            }),
            [members.at(-1), close, indent],
          ]
          for (const [left, right, padding] of boundaries) {
            const separated = left.loc.end.line !== right.loc.start.line
            const prefix = source.lines[right.loc.start.line - 1].slice(0, right.loc.start.column)
            if (separated && prefix === padding) {
              continue
            }
            context.report({
              node: right,
              messageId: 'newline',
              fix(fixer) {
                if (separated && /^\s*$/.test(prefix)) {
                  return fixer.replaceTextRange([right.range[0] - prefix.length, right.range[0]], padding)
                }
                const range = [left.range[1], right.range[0]]
                if (/^\s*$/.test(source.text.slice(...range))) {
                  return fixer.replaceTextRange(range, newline + padding)
                }
                // Keep comments between members; only insert whitespace.
                return fixer.insertTextBefore(right, newline + padding)
              },
            })
          }
        }

        return {
          TSInterfaceBody: check,
          TSTypeLiteral: check,
        }
      },
    },
  },
}
