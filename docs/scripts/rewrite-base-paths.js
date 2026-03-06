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

      // Rewrite href attributes (links, stylesheets, etc.)
      content = content.replace(/href="\/(?!\/|rack-gateway)(.*?)"/g, `href="${BASE_PATH}/$1"`);

      // Rewrite src attributes (images, scripts, etc.)
      content = content.replace(/src="\/(?!\/|rack-gateway)(.*?)"/g, `src="${BASE_PATH}/$1"`);

      // Rewrite srcset attributes (responsive images)
      content = content.replace(/srcset="\/(?!\/|rack-gateway)(.*?)"/g, `srcset="${BASE_PATH}/$1"`);
      // Also handle srcset with multiple sources separated by commas
      content = content.replace(/srcset="([^"]*?)"/g, (match, srcset) => {
        const rewritten = srcset.replace(/(\s|^)\/(?!\/|rack-gateway)([^\s,]+)/g, `$1${BASE_PATH}/$2`);
        return `srcset="${rewritten}"`;
      });

      // Rewrite content attributes (meta tags, etc.)
      content = content.replace(/content="\/(?!\/|rack-gateway)(.*?)"/g, `content="${BASE_PATH}/$1"`);

      // Rewrite data-astro-prefetch href (hero actions)
      content = content.replace(/data-astro-prefetch href="\.\//g, `data-astro-prefetch href="${BASE_PATH}/`);

      await writeFile(fullPath, content, 'utf-8');
      console.log(`Rewrote: ${fullPath}`);
    }
  }
}

async function rewriteCssFiles(dir) {
  const entries = await readdir(dir, { withFileTypes: true });

  for (const entry of entries) {
    const fullPath = join(dir, entry.name);

    if (entry.isDirectory()) {
      await rewriteCssFiles(fullPath);
    } else if (entry.name.endsWith('.css')) {
      let content = await readFile(fullPath, 'utf-8');

      // Rewrite url() in CSS
      content = content.replace(/url\(["']?\/(?!\/|rack-gateway)(.*?)["']?\)/g, `url("${BASE_PATH}/$1")`);

      await writeFile(fullPath, content, 'utf-8');
      console.log(`Rewrote CSS: ${fullPath}`);
    }
  }
}

console.log('Rewriting base paths in HTML files...');
await rewriteHtmlFiles(DIST_DIR);
console.log('Rewriting base paths in CSS files...');
await rewriteCssFiles(DIST_DIR);
console.log('Done!');
