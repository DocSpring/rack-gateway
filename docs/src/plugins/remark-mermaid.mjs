// Turns ```mermaid code blocks into placeholders that src/components/overrides/MarkdownContent.astro renders with
// Mermaid in the browser.
export const mermaidClassName = 'mermaid';

const escapes = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
const escapeHtml = (text) => text.replace(/[&<>"']/g, (char) => escapes[char]);

function replaceMermaidBlocks(node) {
  for (const child of node.children ?? []) {
    if (child.type === 'code' && child.lang === 'mermaid') {
      child.type = 'html';
      child.value = `<div class="${mermaidClassName}" data-content="${escapeHtml(child.value)}"></div>`;
    } else {
      replaceMermaidBlocks(child);
    }
  }
}

export default function remarkMermaid() {
  return replaceMermaidBlocks;
}
