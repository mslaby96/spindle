import { MutationCache, QueryCache, QueryClient, type DefaultOptions } from "@tanstack/react-query";

import { ApiError, meKey } from "./client";

// Any 401 means the session is gone: re-checking /api/me lets the account gate send the user to sign in.
export function createQueryClient(defaultOptions: DefaultOptions = {}) {
  const signedOut = (err: unknown) => {
    if (err instanceof ApiError && err.status === 401) client.invalidateQueries({ queryKey: meKey });
  };
  const client: QueryClient = new QueryClient({
    queryCache: new QueryCache({ onError: (err, query) => query.queryKey[0] !== meKey[0] && signedOut(err) }),
    mutationCache: new MutationCache({ onError: signedOut }),
    defaultOptions,
  });
  return client;
}
