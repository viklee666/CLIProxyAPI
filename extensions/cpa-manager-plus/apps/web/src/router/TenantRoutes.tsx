import { Navigate, useRoutes, type RouteObject } from 'react-router-dom';
import { TenantAccountPage } from '@/features/tenant/TenantAccountPage';
import { TenantDashboardPage } from '@/features/tenant/TenantDashboardPage';
import { TenantGroupsPage } from '@/features/tenant/TenantGroupsPage';
import { TenantKeysPage } from '@/features/tenant/TenantKeysPage';
import { TenantMonitoringPage } from '@/features/tenant/TenantMonitoringPage';
import { TenantProvidersPage } from '@/features/tenant/TenantProvidersPage';
import { TenantUsagePage } from '@/features/tenant/TenantUsagePage';

const tenantRoutes: RouteObject[] = [
  { path: '/', element: <TenantDashboardPage /> },
  { path: '/dashboard', element: <TenantDashboardPage /> },
  { path: '/ai-providers', element: <TenantProvidersPage /> },
  { path: '/client-groups', element: <TenantGroupsPage /> },
  { path: '/client-keys', element: <TenantKeysPage /> },
  { path: '/monitoring', element: <TenantMonitoringPage /> },
  { path: '/usage-analytics', element: <TenantUsagePage /> },
  { path: '/account', element: <TenantAccountPage /> },
  { path: '*', element: <Navigate to="/" replace /> },
];

export function TenantRoutes() {
  return useRoutes(tenantRoutes);
}
