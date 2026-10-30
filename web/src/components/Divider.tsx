import type { CSSProperties, ReactNode } from "react";

import { tabStyle } from "../colors";

export function Divider({ children, color, at }: { children: ReactNode; color?: string; at?: number }) {
  const style = { ...tabStyle(color), ...(at !== undefined && { "--tab-at": at }) } as CSSProperties;
  return (
    <h2 className="divider" style={style}>
      <span>{children}</span>
    </h2>
  );
}
