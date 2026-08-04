import { Navigate, type RouteObject } from 'react-router-dom';
import { MainLayout } from '@/components/layout/MainLayout';
import { DemoPage } from '@/pages/DemoPage';
import { LoginPage } from '@/pages/LoginPage';
import { ProtectedRoute } from '@/router/ProtectedRoute';
import { TenantProtectedRoute } from '@/router/TenantProtectedRoute';
import { TenantLayout } from '@/features/tenant/TenantLayout';
import { TenantLoginPage } from '@/features/tenant/TenantLoginPage';
import { resolvePanelRole } from '@/utils/panelRole';
import { RootShell } from './RootShell';

const demoRoutes: RouteObject[] = [
  { index: true, element: <Navigate to="/demo" replace /> },
  { path: '/demo/*', element: <DemoPage /> },
  { path: '*', element: <Navigate to="/demo" replace /> },
];

const adminRoutes: RouteObject[] = [
  { path: '/login', element: <LoginPage /> },
  {
    path: '/*',
    element: (
      <ProtectedRoute>
        <MainLayout />
      </ProtectedRoute>
    ),
  },
];

const tenantRoutes: RouteObject[] = [
  { path: '/login', element: <TenantLoginPage /> },
  {
    path: '/*',
    element: (
      <TenantProtectedRoute>
        <TenantLayout />
      </TenantProtectedRoute>
    ),
  },
];

export const createAppRoutes = (): RouteObject[] => [
  {
    element: <RootShell />,
    children: __DEMO_SITE__ ? demoRoutes : resolvePanelRole() === 'tenant' ? tenantRoutes : adminRoutes,
  },
];
