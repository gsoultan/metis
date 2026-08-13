import { Component, type ErrorInfo, type ReactNode } from 'react';
import { DefaultErrorFallback } from './DefaultErrorFallback';

interface Props {
  /** Content to protect from unhandled render errors. */
  children: ReactNode;
  /** Custom fallback rendered when an error is caught. Defaults to DefaultErrorFallback. */
  fallback?: ReactNode;
}

interface State {
  hasError: boolean;
  error?: Error;
}

/**
 * FE-ARCH-8: React Error Boundary — class component required by the React API.
 *
 * Wrap route segments and the canvas with this component to prevent one broken
 * component from crashing the entire application.
 *
 * Usage:
 *   <ErrorBoundary>
 *     <ProcessDesigner />
 *   </ErrorBoundary>
 */
export class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props);
    this.state = { hasError: false };
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // In production, forward to your error-reporting service here.
    console.error('[ErrorBoundary] Unhandled render error:', error, info.componentStack);
  }

  private handleReset = () => {
    this.setState({ hasError: false, error: undefined });
  };

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback;
      }
      return <DefaultErrorFallback error={this.state.error} onReset={this.handleReset} />;
    }
    return this.props.children;
  }
}
