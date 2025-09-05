import { expect, test } from '@playwright/test'

test('gateway health via web proxy is OK', async ({ request }) => {
  const res = await request.get('/api/.gateway/health')
  expect(res.ok()).toBeTruthy()
  const json = await res.json()
  expect(json).toBeTruthy()
  // status may be 'ok' depending on backend; assert presence
  expect(json.status).toBeDefined()
})

