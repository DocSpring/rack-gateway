#!/usr/bin/env node
/**
 * Check for broken links in built HTML files.
 *
 * Scans all HTML files and verifies that internal links and local resources
 * (images, CSS, JS, etc.) actually exist on disk.
 */

import { readdir, readFile } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { join, dirname, relative, resolve } from 'node:path';

const DIST_DIR = './dist';
const BASE_PATH = '/rack-gateway';

// URLs that are expected to not exist or should be skipped
const ALLOWLIST: string[] = [
  // Add any known external endpoints or expected 404s here
];

// Source pages to skip checking
const SKIP_PAGES: string[] = [
  // Add any pages with known issues here
];

interface BrokenLink {
  url: string;
  type: 'link' | 'resource';
}

/** Extract all local links and resources from HTML */
function extractLinks(html: string): string[] {
  const allLinks: string[] = [];

  // Extract href from <a> and <link> tags
  const hrefRegex = /<(?:a|link)[^>]+href=["']([^"']+)["']/gi;
  let match: RegExpExecArray | null;
  while ((match = hrefRegex.exec(html)) !== null) {
    allLinks.push(match[1]);
  }

  // Extract src from <img>, <script>, <video>, <audio>, <iframe>, <source>
  const srcRegex = /<(?:img|script|video|audio|iframe|source)[^>]+src=["']([^"']+)["']/gi;
  while ((match = srcRegex.exec(html)) !== null) {
    allLinks.push(match[1]);
  }

  // Extract srcset from responsive images
  const srcsetRegex = /srcset=["']([^"']+)["']/gi;
  while ((match = srcsetRegex.exec(html)) !== null) {
    const srcset = match[1];
    // srcset format: "url1 1x, url2 2x" or "url1 100w, url2 200w"
    for (const part of srcset.split(',')) {
      const url = part.trim().split(/\s+/)[0];
      if (url) {
        allLinks.push(url);
      }
    }
  }

  return allLinks;
}

/** Check if URL is in the allowlist */
function isAllowlisted(url: string): boolean {
  return ALLOWLIST.some(allowed => url.includes(allowed));
}

/** Check if URL is a local link (not external) */
function isLocalLink(url: string): boolean {
  if (!url) return false;

  // Skip data URLs, anchors, javascript, mailto, tel
  if (url.startsWith('data:') || url.startsWith('#') ||
      url.startsWith('javascript:') || url.startsWith('mailto:') ||
      url.startsWith('tel:')) {
    return false;
  }

  // Skip allowlisted URLs
  if (isAllowlisted(url)) {
    return false;
  }

  // Skip external URLs (http/https with domain)
  if (url.match(/^https?:\/\//)) {
    // Allow our production domain
    if (url.startsWith('https://docspring.github.io/rack-gateway')) {
      return true;
    }
    return false;
  }

  return true;
}

/** Convert a URL to a filesystem path */
function urlToFilePath(url: string, htmlDir: string, distDir: string): string | null {
  // Handle absolute URLs to our domain
  if (url.startsWith('https://docspring.github.io/rack-gateway')) {
    url = url.replace('https://docspring.github.io/rack-gateway', '');
  }

  // Remove query string and hash
  url = url.split('?')[0].split('#')[0];

  // URL decode
  url = decodeURIComponent(url);

  // Handle absolute paths (with or without /rack-gateway prefix)
  if (url.startsWith(BASE_PATH)) {
    url = url.slice(BASE_PATH.length);
  }

  if (url.startsWith('/')) {
    const path = url.slice(1);

    // For HTML pages, try multiple candidates
    if (!path.match(/\.(css|js|svg|png|jpg|jpeg|gif|webp|woff|woff2|ttf|eot|ico|mp4|webm|pdf)$/i)) {
      const candidates = [
        join(distDir, path),
        join(distDir, path, 'index.html'),
        join(distDir, path + '.html'),
      ];

      for (const candidate of candidates) {
        if (existsSync(candidate)) {
          return candidate;
        }
      }
      return null;
    }

    // For resources (CSS, JS, images, fonts, etc.), check exact path
    const resourcePath = join(distDir, path);
    return existsSync(resourcePath) ? resourcePath : null;
  }

  // Handle relative paths
  const resolved = resolve(htmlDir, url);
  return existsSync(resolved) ? resolved : null;
}

/** Check a single HTML file for broken links */
async function checkHtmlFile(
  filepath: string,
  distDir: string
): Promise<{ links: BrokenLink[]; checked: number }> {
  const broken: BrokenLink[] = [];
  let content: string;

  try {
    content = await readFile(filepath, 'utf-8');
  } catch (e) {
    console.error(`Could not read file ${filepath}: ${e}`);
    return { links: [], checked: 0 };
  }

  const htmlDir = dirname(filepath);
  const allLinks = extractLinks(content);

  let checked = 0;

  // Check ALL links and resources
  for (const url of allLinks) {
    if (!isLocalLink(url)) continue;
    checked++;

    const targetPath = urlToFilePath(url, htmlDir, distDir);
    if (!targetPath) {
      // Determine type based on URL
      const isResource = url.match(/\.(css|js|svg|png|jpg|jpeg|gif|webp|woff|woff2|ttf|eot|ico|mp4|webm|pdf)$/i);
      broken.push({
        url,
        type: isResource ? 'resource' : 'link'
      });
    }
  }

  return { links: broken, checked };
}

/** Find all HTML files recursively */
async function findHtmlFiles(dir: string, files: string[] = []): Promise<string[]> {
  const entries = await readdir(dir, { withFileTypes: true });

  for (const entry of entries) {
    const fullPath = join(dir, entry.name);
    if (entry.isDirectory()) {
      await findHtmlFiles(fullPath, files);
    } else if (entry.name.endsWith('.html')) {
      files.push(fullPath);
    }
  }

  return files;
}

/** Main function */
async function main() {
  const distDir = resolve(DIST_DIR);

  if (!existsSync(distDir)) {
    console.error(`Error: ${distDir} does not exist`);
    process.exit(1);
  }

  console.log('Checking for broken links in HTML files...\n');

  const brokenLinks: Map<string, BrokenLink[]> = new Map();
  let filesChecked = 0;
  let totalChecked = 0;

  const htmlFiles = await findHtmlFiles(distDir);

  for (const filepath of htmlFiles) {
    const relPath = relative(distDir, filepath);

    // Skip pages with known issues
    if (SKIP_PAGES.some(skip => relPath.includes(skip))) {
      continue;
    }

    filesChecked++;
    const { links: broken, checked } = await checkHtmlFile(filepath, distDir);
    totalChecked += checked;

    if (broken.length > 0) {
      brokenLinks.set(relPath, broken);
    }
  }

  if (brokenLinks.size > 0) {
    console.error(`\n❌ ERROR: Found broken links in ${brokenLinks.size} file(s):\n`);

    for (const [filepath, links] of Array.from(brokenLinks.entries()).sort()) {
      console.error(`  ${filepath}:`);
      const displayLinks = links.slice(0, 5);
      for (const { url, type } of displayLinks) {
        console.error(`    - [${type}] ${url}`);
      }
      if (links.length > 5) {
        console.error(`    ... and ${links.length - 5} more`);
      }
    }

    const totalBroken = Array.from(brokenLinks.values()).reduce((sum, links) => sum + links.length, 0);
    console.error(`\nTotal: ${totalBroken} broken link(s) in ${filesChecked} files (${totalChecked} links checked)`);
    process.exit(1);
  } else {
    console.log(`✅ No broken links found (${filesChecked} files, ${totalChecked} links checked)`);
  }
}

main().catch(err => {
  console.error('Error:', err);
  process.exit(1);
});
