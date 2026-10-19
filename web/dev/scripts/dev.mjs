import { spawn, spawnSync } from 'node:child_process'
import path from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const root = path.resolve(here, '../../..')
const webDir = path.join(root, 'web', 'dev')
const children = []
let shuttingDown = false

function start(name, command, args, cwd) {
  const child = spawn(command, args, {
    cwd,
    stdio: 'inherit',
  })
  children.push({ name, child })
  child.on('exit', (code, signal) => {
    if (shuttingDown) return
    console.log(`\n[dev] ${name} exited (${code ?? signal}) — stopping the other process.`)
    shutdown(typeof code === 'number' ? code : 1)
  })
  child.on('error', (error) => {
    console.error(`[dev] failed to start ${name}: ${error.message}`)
    shutdown(1)
  })
  return child
}

function shutdown(code = 0) {
  if (shuttingDown) return
  shuttingDown = true
  for (const { child } of children) {
    if (child.exitCode !== null) continue
    if (process.platform === 'win32') {
      spawnSync('taskkill', ['/pid', String(child.pid), '/T', '/F'], { stdio: 'ignore' })
    } else {
      try {
        child.kill('SIGTERM')
      } catch {
        /* already gone */
      }
    }
  }
  setTimeout(() => process.exit(code), 250)
}

process.on('SIGINT', () => shutdown(0))
process.on('SIGTERM', () => shutdown(0))

const isWindows = process.platform === 'win32'
const hasAir = spawnSync(isWindows ? 'where' : 'which', ['air'], { stdio: 'ignore' }).status === 0

console.log('[dev] starting Vite (HMR) on http://127.0.0.1:5173/dev/')
start('web', process.execPath, [path.join(webDir, 'node_modules', 'vite', 'bin', 'vite.js'), 'dev', '--port', '5173'], webDir)

if (hasAir) {
  console.log('[dev] starting API with air (hot reload) on http://127.0.0.1:8080')
  start('api', isWindows ? 'air.exe' : 'air', [], root)
} else {
  console.log('[dev] air is not installed; starting the API with `go run` (no hot reload).')
  console.log('[dev] run `make tools` to enable API hot reload.')
  start('api', isWindows ? 'go.exe' : 'go', ['run', './cmd/app', 'serve'], root)
}
