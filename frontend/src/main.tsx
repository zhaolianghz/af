// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { Toaster } from 'react-hot-toast';
import ErrorBoundary from '@/components/shared/ErrorBoundary';
import App from './App';
import './index.css';

const root = document.getElementById('root');
if (!root) {
  throw new Error('Root element #root not found');
}

ReactDOM.createRoot(root).render(
  <React.StrictMode>
    <ErrorBoundary>
      <BrowserRouter>
        <App />
      </BrowserRouter>
      <Toaster
        position="top-right"
        toastOptions={{
          duration: 4_000,
          style: {
            fontSize: '13px',
            fontFamily: 'inherit',
          },
        }}
      />
    </ErrorBoundary>
  </React.StrictMode>,
);
