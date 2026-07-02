// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
import { Navigate, Route, Routes } from 'react-router-dom';
import AdminLayout from '@/layouts/AdminLayout';
import Dashboard from '@/pages/Dashboard';
import Health from '@/pages/Health';
import LoginPage from '@/pages/LoginPage';
import RecommendationsPage from '@/pages/RecommendationsPage';
import PositionsPage from '@/pages/PositionsPage';
import ReviewsPage from '@/pages/ReviewsPage';
import SettingsPage from '@/pages/SettingsPage';
import RunDetailPage from '@/pages/RunDetailPage';
import RunHistoryPage from '@/pages/RunHistoryPage';
import StrategiesPage from '@/pages/StrategiesPage';
import StrategyEditorPage from '@/pages/StrategyEditorPage';
import StrategyNewPage from '@/pages/StrategyNewPage';
import TemplateGalleryPage from '@/pages/TemplateGalleryPage';

export default function App(): JSX.Element {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<AdminLayout />}>
        <Route path="/" element={<Navigate to="/dashboard" replace />} />
        <Route path="/dashboard" element={<Dashboard />} />
        <Route path="/strategies" element={<StrategiesPage />} />
        <Route path="/strategies/new" element={<StrategyNewPage />} />
        <Route path="/strategies/:id" element={<StrategyEditorPage />} />
        <Route path="/templates" element={<TemplateGalleryPage />} />
        <Route path="/runs" element={<RunHistoryPage />} />
        <Route path="/runs/:id" element={<RunDetailPage />} />
        <Route path="/recommendations" element={<RecommendationsPage />} />
        <Route path="/positions" element={<PositionsPage />} />
        <Route path="/reviews" element={<ReviewsPage />} />
        <Route path="/settings" element={<SettingsPage />} />
        <Route path="/health" element={<Health />} />
        <Route
          path="*"
          element={
            <div className="rounded-2xl border border-slate-200 bg-white p-8 text-center shadow-soft">
              <h1 className="text-lg font-semibold text-slate-900">404</h1>
              <p className="mt-2 text-sm text-slate-500">页面不存在</p>
            </div>
          }
        />
      </Route>
    </Routes>
  );
}