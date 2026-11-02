import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router";

import { App } from "./App";
import { ApiError } from "./api/client";
import { createQueryClient } from "./api/queryClient";
import "@fontsource-variable/archivo/wdth.css";
import "./styles.css";

// One more try covers a blip; an error the server meant (4xx) won't change on retry.
const retry = (failures: number, err: unknown) => failures < 1 && !(err instanceof ApiError && err.status < 500);
const queryClient = createQueryClient({ queries: { refetchOnWindowFocus: false, retry } });

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
);
