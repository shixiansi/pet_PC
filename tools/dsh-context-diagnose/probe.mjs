// Replicates dsh-context's probeRunningTree: resolve @deepseek-ai/dsh-session
// as Node would from the plugin's own directory, then read its package.json.
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'

const PLUGIN_LIB = 'C:/Users/fan/.dsh/profiles/web/node_modules/dsh-context/lib'
const PROBE = ['@deepseek-ai/dsh-session', '@deepseek-ai/dsh-session-projection']

// Walk node_modules from PLUGIN_LIB upward, exactly like Node's algorithm.
function findPackage(name, from) {
  let dir = from
  while (true) {
    const cand = join(dir, 'node_modules', name, 'package.json')
    try {
      readFileSync(cand, 'utf8')
      return cand
    } catch { /* not here */ }
    const parent = join(dir, '..')
    if (parent === dir) return undefined
    dir = parent
  }
}

for (const name of PROBE) {
  const manifest = findPackage(name, PLUGIN_LIB)
  if (!manifest) {
    console.log(`${name} => NOT FOUND walking up`)
    continue
  }
  const version = JSON.parse(readFileSync(manifest, 'utf8')).version
  console.log(`${name}\n  manifest: ${manifest}\n  version : ${version}`)
}

// What the plugin compares against
console.log('\nBASELINE_DSH_VERSION = 0.1.7-rc.2')
console.log('meetsBaseline(0.2.0-rc.2) => true   => full registration')
