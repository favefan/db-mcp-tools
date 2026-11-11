import { Component, type ErrorInfo, type ReactNode } from "react";
import { DiagnosticExport } from "./diagnostic-export";
import { reportFrontend } from "../lib/diagnostics";

export class ErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  componentDidCatch(error: Error, info: ErrorInfo) {
    reportFrontend("react", error, `${error.stack ?? ""}\n${info.componentStack ?? ""}`);
  }
  render() {
    return this.state.failed ? (
      <main className="settings-content">
        <h1>界面暂时无法显示</h1>
        <p>错误已记录。请导出诊断包后重新打开客户端。</p>
        <DiagnosticExport />
      </main>
    ) : (
      this.props.children
    );
  }
}
