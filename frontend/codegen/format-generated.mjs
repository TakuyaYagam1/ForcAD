import { readdir, readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { ESLint } from 'eslint'
import tseslint from 'typescript-eslint'
import { multilineRules } from '../eslint.multiline.js'
import { multilinePlugin } from '../eslint.multiline-types.js'

const frontendRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const formatter = new ESLint({
  cwd: frontendRoot,
  overrideConfigFile: true,
  fix: true,
  overrideConfig: [{
    files: ['**/*.ts'],
    plugins: {
      multiline: multilinePlugin
    },
    languageOptions: {
      parser: tseslint.parser
    },
    rules: multilineRules,
  }],
})

export async function formatGeneratedSource(source) {
  const canonical = source.replace(/\r\n?/g, '\n')
  const [result] = await formatter.lintText(canonical, {
    filePath: path.join(frontendRoot, 'src/shared/api/generated/client.ts'),
  })
  if (result.errorCount) {
    const message = result.messages[0]
    throw new Error(`Generated client style: ${message.ruleId}: ${message.message}`)
  }
  return (result.output ?? canonical).trimEnd() + '\n'
}

export async function formatGeneratedClient(paths) {
  if (!Array.isArray(paths)) {
    throw new TypeError('Expected generated paths')
  }
  for (const filename of paths) {
    if (typeof filename !== 'string' || !filename.endsWith('.ts')) {
      continue
    }
    const source = await readFile(filename, 'utf8')
    await writeFile(filename, await formatGeneratedSource(source))
  }
}

async function generatedFiles(directory) {
  const files = []
  for (const entry of await readdir(directory, {
    withFileTypes: true
  })) {
    const filename = path.join(directory, entry.name)
    if (entry.isDirectory()) {
      files.push(...await generatedFiles(filename))
    } else if (entry.isFile() && filename.endsWith('.ts')) {
      files.push(filename)
    }
  }
  return files
}

export async function formatAllGeneratedClient(directory = path.join(frontendRoot, 'src/shared/api/generated')) {
  await formatGeneratedClient(await generatedFiles(directory))
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await formatAllGeneratedClient()
}
