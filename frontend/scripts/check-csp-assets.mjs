import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const html = readFileSync(
  fileURLToPath(new URL('../dist/index.html', import.meta.url)),
  'utf8',
);
const asset = /^\/?[A-Za-z0-9_-]+\.[0-9a-f]{20}\.(js|css)$/;
const scripts = [...html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/gi)];
const styles = [
  ...html.matchAll(/<link\b([^>]*rel=["']?stylesheet["']?[^>]*)>/gi),
];
if (
  !scripts.length ||
  !styles.length ||
  /\s(?:on\w+|style)\s*=|<base\b|<iframe\b|<object\b/i.test(html)
) {
  throw new Error('Production HTML violates the strict asset policy.');
}
for (const [, attributes, content = ''] of [...scripts, ...styles]) {
  const match = attributes.match(
    /(?:src|href)=(?:["']([^"']+)["']|([^\s>]+))/i,
  );
  const source = match?.[1] ?? match?.[2];
  if (!source || !asset.test(source) || content.trim()) {
    throw new Error(
      'Production scripts/styles must be external same-origin hashed assets.',
    );
  }
}
console.log(
  'Production entry uses external same-origin scripts and extracted CSS.',
);
