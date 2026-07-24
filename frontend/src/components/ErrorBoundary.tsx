import { Component, type ErrorInfo, type ReactNode } from "react";

interface Props {
  children: ReactNode;
}

interface State {
  error: Error | null;
}

/** Catch render failures at the application boundary and offer a recoverable retry. */
export default class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("frontend render error", error, info.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <main className="state-card error-state" role="alert">
        <h2>页面出现异常</h2>
        <p>当前页面无法正常显示，请重试；你的登录状态不会因此丢失。</p>
        <button type="button" onClick={() => this.setState({ error: null })}>
          重试
        </button>
      </main>
    );
  }
}
