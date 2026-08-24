import { lazy } from 'react';
import { Navigate, useRoutes, type Location } from 'react-router-dom';

const DashboardPage = lazy(() =>
  import('@/pages/DashboardPage').then(({ DashboardPage }) => ({ default: DashboardPage }))
);
const AiProvidersPage = lazy(() =>
  import('@/pages/AiProvidersPage').then(({ AiProvidersPage }) => ({ default: AiProvidersPage }))
);
const AiProvidersAmpcodeEditPage = lazy(() =>
  import('@/pages/AiProvidersAmpcodeEditPage').then(({ AiProvidersAmpcodeEditPage }) => ({
    default: AiProvidersAmpcodeEditPage,
  }))
);
const AiProvidersClaudeEditLayout = lazy(() =>
  import('@/pages/AiProvidersClaudeEditLayout').then(({ AiProvidersClaudeEditLayout }) => ({
    default: AiProvidersClaudeEditLayout,
  }))
);
const AiProvidersClaudeEditPage = lazy(() =>
  import('@/pages/AiProvidersClaudeEditPage').then(({ AiProvidersClaudeEditPage }) => ({
    default: AiProvidersClaudeEditPage,
  }))
);
const AiProvidersClaudeModelsPage = lazy(() =>
  import('@/pages/AiProvidersClaudeModelsPage').then(({ AiProvidersClaudeModelsPage }) => ({
    default: AiProvidersClaudeModelsPage,
  }))
);
const AiProvidersCodexEditPage = lazy(() =>
  import('@/pages/AiProvidersCodexEditPage').then(({ AiProvidersCodexEditPage }) => ({
    default: AiProvidersCodexEditPage,
  }))
);
const AiProvidersGeminiEditPage = lazy(() =>
  import('@/pages/AiProvidersGeminiEditPage').then(({ AiProvidersGeminiEditPage }) => ({
    default: AiProvidersGeminiEditPage,
  }))
);
const AiProvidersOpenAIEditLayout = lazy(() =>
  import('@/pages/AiProvidersOpenAIEditLayout').then(({ AiProvidersOpenAIEditLayout }) => ({
    default: AiProvidersOpenAIEditLayout,
  }))
);
const AiProvidersOpenAIEditPage = lazy(() =>
  import('@/pages/AiProvidersOpenAIEditPage').then(({ AiProvidersOpenAIEditPage }) => ({
    default: AiProvidersOpenAIEditPage,
  }))
);
const AiProvidersOpenAIModelsPage = lazy(() =>
  import('@/pages/AiProvidersOpenAIModelsPage').then(({ AiProvidersOpenAIModelsPage }) => ({
    default: AiProvidersOpenAIModelsPage,
  }))
);
const AiProvidersVertexEditPage = lazy(() =>
  import('@/pages/AiProvidersVertexEditPage').then(({ AiProvidersVertexEditPage }) => ({
    default: AiProvidersVertexEditPage,
  }))
);
const AuthFilesPage = lazy(() =>
  import('@/pages/AuthFilesPage').then(({ AuthFilesPage }) => ({ default: AuthFilesPage }))
);
const AuthFilesOAuthExcludedEditPage = lazy(() =>
  import('@/pages/AuthFilesOAuthExcludedEditPage').then(({ AuthFilesOAuthExcludedEditPage }) => ({
    default: AuthFilesOAuthExcludedEditPage,
  }))
);
const AuthFilesOAuthModelAliasEditPage = lazy(() =>
  import('@/pages/AuthFilesOAuthModelAliasEditPage').then(({ AuthFilesOAuthModelAliasEditPage }) => ({
    default: AuthFilesOAuthModelAliasEditPage,
  }))
);
const OAuthPage = lazy(() => import('@/pages/OAuthPage').then(({ OAuthPage }) => ({ default: OAuthPage })));
const QuotaPage = lazy(() => import('@/pages/QuotaPage').then(({ QuotaPage }) => ({ default: QuotaPage })));
const MonitoringCenterPage = lazy(() =>
  import('@/pages/MonitoringCenterPage').then(({ MonitoringCenterPage }) => ({
    default: MonitoringCenterPage,
  }))
);
const CodexInspectionPage = lazy(() =>
  import('@/pages/CodexInspectionPage').then(({ CodexInspectionPage }) => ({
    default: CodexInspectionPage,
  }))
);
const ConfigPage = lazy(() => import('@/pages/ConfigPage').then(({ ConfigPage }) => ({ default: ConfigPage })));
const LogsPage = lazy(() => import('@/pages/LogsPage').then(({ LogsPage }) => ({ default: LogsPage })));
const SystemPage = lazy(() => import('@/pages/SystemPage').then(({ SystemPage }) => ({ default: SystemPage })));
const EnterpriseKeysPage = lazy(() =>
  import('@/pages/EnterpriseKeysPage').then(({ EnterpriseKeysPage }) => ({ default: EnterpriseKeysPage }))
);
const QuotaLimitsPage = lazy(() =>
  import('@/pages/QuotaLimitsPage').then(({ QuotaLimitsPage }) => ({ default: QuotaLimitsPage }))
);
const QuotaDowngradePage = lazy(() =>
  import('@/pages/QuotaDowngradePage').then(({ QuotaDowngradePage }) => ({
    default: QuotaDowngradePage,
  }))
);
const AlertConfigPage = lazy(() =>
  import('@/pages/AlertConfigPage').then(({ AlertConfigPage }) => ({ default: AlertConfigPage }))
);
const ApiKeyUsageSelfServicePage = lazy(() =>
  import('@/pages/ApiKeyUsageSelfServicePage').then(({ ApiKeyUsageSelfServicePage }) => ({
    default: ApiKeyUsageSelfServicePage,
  }))
);

const mainRoutes = [
  { path: '/', element: <DashboardPage /> },
  { path: '/dashboard', element: <DashboardPage /> },
  { path: '/settings', element: <Navigate to="/config" replace /> },
  { path: '/api-keys', element: <Navigate to="/config" replace /> },
  { path: '/ai-providers/gemini/new', element: <AiProvidersGeminiEditPage /> },
  { path: '/ai-providers/gemini/:index', element: <AiProvidersGeminiEditPage /> },
  { path: '/ai-providers/codex/new', element: <AiProvidersCodexEditPage /> },
  { path: '/ai-providers/codex/:index', element: <AiProvidersCodexEditPage /> },
  {
    path: '/ai-providers/claude/new',
    element: <AiProvidersClaudeEditLayout />,
    children: [
      { index: true, element: <AiProvidersClaudeEditPage /> },
      { path: 'models', element: <AiProvidersClaudeModelsPage /> },
    ],
  },
  {
    path: '/ai-providers/claude/:index',
    element: <AiProvidersClaudeEditLayout />,
    children: [
      { index: true, element: <AiProvidersClaudeEditPage /> },
      { path: 'models', element: <AiProvidersClaudeModelsPage /> },
    ],
  },
  { path: '/ai-providers/vertex/new', element: <AiProvidersVertexEditPage /> },
  { path: '/ai-providers/vertex/:index', element: <AiProvidersVertexEditPage /> },
  {
    path: '/ai-providers/openai/new',
    element: <AiProvidersOpenAIEditLayout />,
    children: [
      { index: true, element: <AiProvidersOpenAIEditPage /> },
      { path: 'models', element: <AiProvidersOpenAIModelsPage /> },
    ],
  },
  {
    path: '/ai-providers/openai/:index',
    element: <AiProvidersOpenAIEditLayout />,
    children: [
      { index: true, element: <AiProvidersOpenAIEditPage /> },
      { path: 'models', element: <AiProvidersOpenAIModelsPage /> },
    ],
  },
  { path: '/ai-providers/ampcode', element: <AiProvidersAmpcodeEditPage /> },
  { path: '/ai-providers', element: <AiProvidersPage /> },
  { path: '/ai-providers/*', element: <AiProvidersPage /> },
  { path: '/auth-files', element: <AuthFilesPage /> },
  { path: '/auth-files/oauth-excluded', element: <AuthFilesOAuthExcludedEditPage /> },
  { path: '/auth-files/oauth-model-alias', element: <AuthFilesOAuthModelAliasEditPage /> },
  { path: '/oauth', element: <OAuthPage /> },
  { path: '/quota', element: <QuotaPage /> },
  { path: '/monitoring', element: <MonitoringCenterPage /> },
  { path: '/monitoring/codex-inspection', element: <CodexInspectionPage /> },
  { path: '/config', element: <ConfigPage /> },
  { path: '/logs', element: <LogsPage /> },
  { path: '/system', element: <SystemPage /> },
  { path: '/enterprise-keys', element: <EnterpriseKeysPage /> },
  { path: '/quota-limits', element: <QuotaLimitsPage /> },
  { path: '/quota-downgrade', element: <QuotaDowngradePage /> },
  { path: '/quota-paused', element: <Navigate to="/quota-limits" replace /> },
  { path: '/my-usage', element: <ApiKeyUsageSelfServicePage /> },
  { path: '/alert-config', element: <AlertConfigPage /> },
  { path: '*', element: <Navigate to="/" replace /> },
];

export function MainRoutes({ location }: { location?: Location }) {
  return useRoutes(mainRoutes, location);
}
