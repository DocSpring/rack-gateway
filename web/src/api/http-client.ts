import type { AxiosRequestConfig, AxiosResponse } from 'axios';

import { getHttpClientInstance } from '@/contexts/http-client-context';

export function createGatewayClient<T>(
  config: AxiosRequestConfig,
  options?: AxiosRequestConfig,
): Promise<AxiosResponse<T>> {
  const client = getHttpClientInstance();

  const mergedConfig: AxiosRequestConfig = {
    ...config,
    ...options,
    headers: {
      ...(config.headers ?? {}),
      ...(options?.headers ?? {}),
    },
  };

  return client.request<T>(mergedConfig);
}
