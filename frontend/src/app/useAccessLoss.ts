import { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { ApiError } from '../api/client';

// Forbidden feature views hide data immediately, then discard this scope's cache
// and ask the outer membership boundary to revalidate before rendering it again.
export function useAccessLoss(error: unknown, refreshAccess: () => void) {
  const client = useQueryClient();
  useEffect(() => {
    if (error instanceof ApiError && [403, 404].includes(error.status)) {
      void client.cancelQueries();
      client.clear();
      refreshAccess();
    }
  }, [error, client, refreshAccess]);
}
