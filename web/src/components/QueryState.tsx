import { useEffect, useState } from "react";

type Query = { error: Error | null; refetch: () => unknown };

const loadingDelayMs = 300;

// What a page shows while its data isn't there: the error with a retry, or after a moment, that it is loading.
export function QueryState({ query, what }: { query: Query; what: string }) {
  const [slow, setSlow] = useState(false);
  useEffect(() => {
    const t = setTimeout(() => setSlow(true), loadingDelayMs);
    return () => clearTimeout(t);
  }, []);

  if (query.error) {
    return (
      <div className="load-error" role="alert">
        <p>Couldn't load {what}.</p>
        <p className="muted">{query.error.message}</p>
        <button className="secondary" onClick={() => query.refetch()}>
          Try again
        </button>
      </div>
    );
  }
  return slow ? (
    <p className="loading" role="status">
      Loading…
    </p>
  ) : null;
}
