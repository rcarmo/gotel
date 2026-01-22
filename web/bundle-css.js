// CSS Bundler Script
// Bundles perf-cascade.css and styles.css into a single bundle.css file

const perfCascade = await Bun.file('/workspace/web/node_modules/perf-cascade/dist/perf-cascade.css').text();
const styles = await Bun.file('/workspace/web/styles.css').text();
const bundle = perfCascade + '\n' + styles;
await Bun.write('/workspace/web/dist/bundle.css', bundle);

console.log('✅ CSS bundle created successfully!');
console.log(`Bundle size: ${bundle.length} bytes`);
console.log('📁 Output: /workspace/web/dist/bundle.css');