import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import { RatingStars } from "./RatingStars";

function Harness({ start }: { start?: number }) {
  const [value, setValue] = useState<number | undefined>(start);
  return (
    <>
      <button>before</button>
      <RatingStars value={value} onChange={setValue} />
      <button>after</button>
    </>
  );
}

describe("rating with the keyboard", () => {
  it("is one stop for Tab", async () => {
    render(<Harness />);
    await userEvent.click(screen.getByRole("button", { name: "before" }));
    await userEvent.tab();
    expect(screen.getByRole("radio", { name: "0.5 stars" })).toHaveFocus();
    await userEvent.tab();
    expect(screen.getByRole("button", { name: "after" })).toHaveFocus();
  });

  it("lands on the current rating and moves by half a star with the arrows", async () => {
    render(<Harness start={6} />);
    await userEvent.click(screen.getByRole("button", { name: "before" }));
    await userEvent.tab();
    expect(screen.getByRole("radio", { name: "3 stars" })).toHaveFocus();
    await userEvent.keyboard("{ArrowRight}");
    expect(screen.getByRole("radio", { name: "3.5 stars" })).toHaveFocus();
    expect(screen.getByRole("radio", { name: "3.5 stars" })).toBeChecked();
    await userEvent.keyboard("{ArrowLeft}{ArrowLeft}");
    expect(screen.getByRole("radio", { name: "2.5 stars" })).toBeChecked();
    await userEvent.keyboard("{End}");
    expect(screen.getByRole("radio", { name: "5 stars" })).toBeChecked();
    await userEvent.keyboard("{ArrowRight}");
    expect(screen.getByRole("radio", { name: "5 stars" })).toBeChecked();
    await userEvent.keyboard("{Home}");
    expect(screen.getByRole("radio", { name: "0.5 stars" })).toBeChecked();
  });
});
