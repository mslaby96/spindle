import { render } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { vi } from "vitest";

import { App } from "./App";
import { createQueryClient } from "./api/queryClient";

type Handler = (body: unknown) => [number, unknown?];

export function mockApi(routes: Record<string, Handler>) {
  const calls: { key: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const key = `${init?.method ?? "GET"} ${path}`;
      const body = init?.body ? JSON.parse(init.body as string) : undefined;
      calls.push({ key, body });
      const headers = new Headers(init?.headers);
      if (!key.startsWith("GET ") && headers.get("X-Spindle") === null) {
        return new Response(JSON.stringify({ error: "missing X-Spindle header" }), { status: 403 });
      }
      if (body !== undefined && headers.get("Content-Type") !== "application/json") {
        return new Response(JSON.stringify({ error: "expected JSON" }), { status: 415 });
      }
      const handler = routes[key];
      const [status, json] = handler ? handler(body) : [404, { error: "not found" }];
      return new Response(status === 204 ? null : JSON.stringify(json ?? {}), { status });
    }),
  );
  return calls;
}

export class FakeEventSource {
  static CLOSED = 2;
  static instances: FakeEventSource[] = [];
  readyState = 1;
  onerror: (() => void) | null = null;
  onopen: (() => void) | null = null;
  private listeners: Record<string, ((e: MessageEvent) => void)[]> = {};

  constructor(public url: string) {
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, fn: (e: MessageEvent) => void) {
    (this.listeners[type] ??= []).push(fn);
  }

  emit(type: string, data: unknown) {
    for (const fn of this.listeners[type] ?? []) fn(new MessageEvent(type, { data: JSON.stringify(data) }));
  }

  open() {
    this.readyState = 1;
    this.onopen?.();
  }

  close() {
    this.readyState = FakeEventSource.CLOSED;
  }
}

export function renderApp(path = "/") {
  FakeEventSource.instances = [];
  vi.stubGlobal("EventSource", FakeEventSource);
  const client = createQueryClient({ queries: { retry: false } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
