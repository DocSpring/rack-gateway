import { defineConfig } from 'orval'

export default defineConfig({
  gateway: {
    input: '../internal/gateway/openapi/generated/swagger.json',
    output: {
      mode: 'single',
      target: 'src/api/generated.ts',
      schemas: 'src/api/schemas',
      client: 'axios',
      prettier: true,
      override: {
        mutator: {
          path: 'src/api/http-client.ts',
          name: 'createGatewayClient',
        },
      },
    },
  },
})
