import { lazy, Suspense, useEffect } from 'react';
import { Outlet, RouterProvider, createHashRouter } from 'react-router-dom';
import { LoginPage } from '@/pages/LoginPage';
import { NotificationContainer } from '@/components/common/NotificationContainer';
import { ConfirmationModal } from '@/components/common/ConfirmationModal';
import { MainLayout } from '@/components/layout/MainLayout';
import { ProtectedRoute } from '@/router/ProtectedRoute';
import { useLanguageStore, useThemeStore } from '@/stores';

const ApiKeyUsageSelfServicePage = lazy(() =>
  import('@/pages/ApiKeyUsageSelfServicePage').then(({ ApiKeyUsageSelfServicePage }) => ({
    default: ApiKeyUsageSelfServicePage,
  }))
);

function RootShell() {
  return (
    <>
      <NotificationContainer />
      <ConfirmationModal />
      <Suspense fallback={<div className="page-loading">Loading...</div>}>
        <Outlet />
      </Suspense>
    </>
  );
}

const router = createHashRouter([
  {
    element: <RootShell />,
    children: [
      { path: '/login', element: <LoginPage /> },
      { path: '/my-usage', element: <ApiKeyUsageSelfServicePage /> },
      {
        path: '/*',
        element: (
          <ProtectedRoute>
            <MainLayout />
          </ProtectedRoute>
        ),
      },
    ],
  },
]);

function App() {
  const initializeTheme = useThemeStore((state) => state.initializeTheme);
  const language = useLanguageStore((state) => state.language);
  const setLanguage = useLanguageStore((state) => state.setLanguage);

  useEffect(() => {
    const cleanupTheme = initializeTheme();
    return cleanupTheme;
  }, [initializeTheme]);

  useEffect(() => {
    setLanguage(language);
  }, []);

  useEffect(() => {
    document.documentElement.lang = language;
  }, [language]);

  return <RouterProvider router={router} />;
}

export default App;