// CSS Bundler Script
// Bundles perf-cascade.css and styles.css into a single bundle.css file

import { mkdir } from 'fs/promises';
import { dirname, resolve } from 'path';
import { fileURLToPath } from 'url';

const currentDir = dirname(fileURLToPath(import.meta.url));
const distDir = resolve(currentDir, 'dist');
const perfCascadePath = resolve(currentDir, 'node_modules/perf-cascade/dist/perf-cascade.css');
const stylesPath = resolve(currentDir, 'styles.css');
const outputPath = resolve(distDir, 'bundle.css');

const perfCascade = await Bun.file(perfCascadePath).text();
const styles = await Bun.file(stylesPath).text();
const bundle = perfCascade + '\n' + styles;

await mkdir(distDir, { recursive: true });
await Bun.write(outputPath, bundle);

console.log('✅ CSS bundle created successfully!');
console.log(`Bundle size: ${bundle.length} bytes`);
console.log(`📁 Output: ${outputPath}`);
