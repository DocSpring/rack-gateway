#!/usr/bin/env node

import { readdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

const BASE_PATH = '/rack-gateway';
const DIST_DIR = './dist';

async function rewriteHtmlFiles(dir) {
  const entries = await readdir(dir, { withFileTypes: true });

  for (const entry of entries) {
    const fullPath = join(dir, entry.name);

    if (entry.isDirectory()) {
      await rewriteHtmlFiles(fullPath);
    } else if (entry.name.endsWith('.html')) {
      let content = await readFile(fullPath, 'utf-8');

      // Rewrite internal links (href="/" or href="/something")
      content = content.replace(/href="\/(?!\/|rack-gateway)(.*?)"/g, `href="${BASE_PATH}/$1"`);

      // Rewrite action links in hero
      content = content.replace(/data-astro-prefetch href="\.\//g, `data-astro-prefetch href="${BASE_PATH}/`);

      await writeFile(fullPath, content, 'utf-8');
      console.log(`Rewrote: ${fullPath}`);
    }
  }
}

console.log('Rewriting base paths in HTML files...');
await rewriteHtmlFiles(DIST_DIR);
console.log('Done!');
