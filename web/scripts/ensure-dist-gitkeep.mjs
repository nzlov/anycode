import { mkdir, writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = dirname(fileURLToPath(import.meta.url));
const gitkeepPath = resolve(scriptDir, '../../internal/interfaces/http/static/dist/.gitkeep');

await mkdir(dirname(gitkeepPath), { recursive: true });
await writeFile(gitkeepPath, '');
