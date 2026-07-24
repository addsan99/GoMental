import { spawnSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { dirname, join } from 'node:path';

// Resolve npm-cli.js relative to the node binary that is running this script,
// rather than hardcoding a path (the install location varies by machine and node
// version manager). Windows ships npm as a sibling of node.exe; Unix keeps it
// under lib/. Fall back to the PATH-resolved `npm` shim if neither is found.
const nodeDir = dirname(process.execPath);
const candidates = [
  join(nodeDir, 'node_modules', 'npm', 'bin', 'npm-cli.js'),
  join(nodeDir, '..', 'lib', 'node_modules', 'npm', 'bin', 'npm-cli.js'),
];
const npmCli = candidates.find(existsSync);

const result = npmCli
  ? spawnSync(process.execPath, [npmCli, 'install'], {
      cwd: process.cwd(),
      env: process.env,
      stdio: 'inherit',
      windowsHide: true,
    })
  : spawnSync('npm', ['install'], {
      cwd: process.cwd(),
      env: process.env,
      stdio: 'inherit',
      windowsHide: true,
      shell: true,
    });

if (result.error) {
  console.error(result.error.message);
  process.exit(1);
}

process.exit(result.status ?? 0);
