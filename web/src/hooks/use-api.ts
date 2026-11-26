"use client";

import { useCallback, useEffect, useState } from "react";
import { api, errorMessage } from "@/lib/api";

interface State<T> {
  key: string | null; // path + reload nonce the data belongs to
  data?: T;
  error: string | null;
}

/** Fetch JSON from the API; `path` null skips fetching. State only changes in
 * async callbacks, so effects never set state synchronously. */
export function useApi<T>(path: string | null) {
  const [nonce, setNonce] = useState(0);
  const [state, setState] = useState<State<T>>({ key: null, error: null });
  const key = path === null ? null : `${path}#${nonce}`;

  useEffect(() => {
    if (path === null) return;
    let cancelled = false;
    api<T>(path).then(
      (data) => !cancelled && setState({ key: `${path}#${nonce}`, data, error: null }),
      (e) => !cancelled && setState((s) => ({ key: `${path}#${nonce}`, data: s.data, error: errorMessage(e) })),
    );
    return () => {
      cancelled = true;
    };
  }, [path, nonce]);

  const setData = useCallback((data: T) => setState((s) => ({ ...s, data })), []);
  const reload = useCallback(() => setNonce((n) => n + 1), []);

  return {
    data: state.data,
    setData,
    error: state.error,
    loading: key !== null && state.key !== key,
    reload,
  };
}

/** Debounce a value (search boxes). */
export function useDebounced<T>(value: T, ms = 250): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}
