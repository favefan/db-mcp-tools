import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import App from "./App";
import "./styles.css";
import { installErrorReporting } from "./lib/diagnostics";
import { ErrorBoundary } from "./components/error-boundary";

installErrorReporting();

const client = new QueryClient({
  defaultOptions: { queries: { retry: false, staleTime: 3000, refetchOnWindowFocus: false } },
});
ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={client}>
      <ErrorBoundary>
        <App />
      </ErrorBoundary>
    </QueryClientProvider>
  </React.StrictMode>,
);
