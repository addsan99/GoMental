// Minimal test runner for pure frontend modules.
//
// The app has no vitest/jest setup, and adding one is a bigger decision than
// this change warrants. Instead we bundle each `*.test.ts` with the esbuild that
// vite already depends on and run it under Node. That covers pure logic
// (filters, parsing, formatting); anything needing a DOM or React still needs a
// real test framework.
import {execFileSync} from 'node:child_process'
import {mkdtempSync, rmSync, readdirSync} from 'node:fs'
import {tmpdir} from 'node:os'
import path from 'node:path'
import {fileURLToPath, pathToFileURL} from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const root = path.resolve(here, '..')

function findTests(dir) {
  const out = []
  for (const entry of readdirSync(dir, {withFileTypes: true})) {
    if (entry.name === 'node_modules' || entry.name.startsWith('.')) continue
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) out.push(...findTests(full))
    else if (/\.test\.tsx?$/.test(entry.name)) out.push(full)
  }
  return out
}

const files = findTests(path.join(root, 'src')).sort()
if (files.length === 0) {
  console.error('no *.test.ts files found under src/')
  process.exit(1)
}

const outDir = mkdtempSync(path.join(tmpdir(), 'gomental-tests-'))
let passed = 0
let failed = 0

try {
  for (const file of files) {
    const rel = path.relative(root, file)
    const bundle = path.join(outDir, rel.replace(/[\\/]/g, '_').replace(/\.tsx?$/, '.mjs'))
    execFileSync(
      path.join(root, 'node_modules/.bin/esbuild'),
      [
        file,
        '--bundle',
        '--platform=node',
        '--format=esm',
        '--jsx=automatic',
        '--loader:.css=empty',
        `--outfile=${bundle}`,
        '--log-level=warning',
      ],
      {stdio: ['ignore', 'ignore', 'inherit']},
    )

    const mod = await import(pathToFileURL(bundle).href)
    const tests = mod.tests || {}
    const names = Object.keys(tests)
    if (names.length === 0) {
      console.error(`  ${rel}: no exported \`tests\` object`)
      failed += 1
      continue
    }
    console.log(rel)
    for (const name of names) {
      try {
        await tests[name]()
        console.log(`  ok  ${name}`)
        passed += 1
      } catch (err) {
        console.log(`  FAIL ${name}`)
        console.log(String(err?.stack || err).split('\n').map((l) => `       ${l}`).join('\n'))
        failed += 1
      }
    }
  }
} finally {
  rmSync(outDir, {recursive: true, force: true})
}

console.log(`\n${passed} passed, ${failed} failed`)
process.exit(failed === 0 ? 0 : 1)
