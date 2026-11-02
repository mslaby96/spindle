import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { QueryState } from "./QueryState";

afterEach(() => vi.useRealTimers());

describe("QueryState", () => {
  it("says what could not be loaded, why, and tries again", async () => {
    const refetch = vi.fn();
    render(<QueryState query={{ error: new Error("server unreachable"), refetch }} what="your diary" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Couldn't load your diary.");
    expect(screen.getByText("server unreachable")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(refetch).toHaveBeenCalledOnce();
  });

  it("shows loading only when it takes a moment, so quick loads don't flash", () => {
    vi.useFakeTimers();
    render(<QueryState query={{ error: null, refetch: vi.fn() }} what="your diary" />);
    expect(screen.queryByText("Loading…")).not.toBeInTheDocument();
    act(() => vi.advanceTimersByTime(300));
    expect(screen.getByRole("status")).toHaveTextContent("Loading…");
  });
});
