export function formatCodeForDownload(codes: string[]): string {
  const header = [
    'Your Rack Gateway backup codes',
    '',
    'Each code can be used once. Store them securely.',
    '',
  ]
  return [...header, ...codes].join('\n')
}
