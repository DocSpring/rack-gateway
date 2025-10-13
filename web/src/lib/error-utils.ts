import { isAxiosError } from 'axios';

import { toast } from '@/components/ui/use-toast';
import { isMFAError } from '@/contexts/step-up-context';

export function getErrorMessage(
  error: unknown,
  fallback = 'Something went wrong',
): string {
  const resolved = resolveErrorMessage(error);
  if (typeof resolved === 'string' && resolved.trim() !== '') {
    return resolved;
  }
  return fallback;
}

export function toastAPIError(
  error: unknown,
  fallback = 'Something went wrong',
): void {
  if (isMFAError(error)) {
    return;
  }
  toast.error(getErrorMessage(error, fallback));
}

export function withAPIErrorMessage(
  error: unknown,
  fallback: string,
  handler: (message: string) => void,
): void {
  if (isMFAError(error)) {
    return;
  }
  handler(getErrorMessage(error, fallback));
}

function resolveErrorMessage(error: unknown): string | undefined {
  if (typeof error === 'string') {
    return error;
  }

  const axiosMessage = messageFromAxios(error);
  if (axiosMessage) {
    return axiosMessage;
  }

  if (error instanceof Error) {
    return error.message;
  }

  if (typeof error === 'object' && error !== null) {
    const message = (error as { message?: string }).message;
    if (typeof message === 'string' && message.trim() !== '') {
      return message;
    }
  }

  return;
}

function messageFromAxios(error: unknown): string | undefined {
  if (!isAxiosError<{ error?: string; message?: string }>(error)) {
    return;
  }
  const data = error.response?.data as
    | string
    | { message?: string | undefined; error?: string | undefined }
    | undefined;

  if (typeof data === 'string') {
    return data.trim() !== '' ? data : undefined;
  }

  if (data && typeof data === 'object') {
    const message =
      typeof data.message === 'string' ? data.message : data.error;
    if (typeof message === 'string' && message.trim() !== '') {
      return message;
    }
  }

  return;
}
