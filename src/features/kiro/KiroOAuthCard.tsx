import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { Input } from '@/components/ui/Input';
import { IconPlug } from '@/components/ui/icons';
import { notifyAuthFilesChanged } from '@/features/authFiles/authFilesEvents';
import { kiroApi, oauthApi, type KiroAuthMethod, type KiroConnectRequest } from '@/services/api';
import { useNotificationStore } from '@/stores';
import { getErrorMessage } from '@/utils/helpers';
import styles from './KiroOAuthCard.module.scss';

interface KiroOAuthCardProps {
  title: string;
  icon: string;
}

interface KiroFormState {
  startURL: string;
  region: string;
  apiKey: string;
  refreshAuthMethod: 'builder-id' | 'idc';
  refreshToken: string;
  clientID: string;
  clientSecret: string;
  credentialJSON: string;
}

const DEFAULT_FORM: KiroFormState = {
  startURL: '',
  region: 'us-east-1',
  apiKey: '',
  refreshAuthMethod: 'builder-id',
  refreshToken: '',
  clientID: '',
  clientSecret: '',
  credentialJSON: '',
};

const METHODS: KiroAuthMethod[] = ['builder-id', 'idc', 'api_key', 'refresh_token', 'external_idp'];

export function KiroOAuthCard({ title, icon }: KiroOAuthCardProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { showNotification } = useNotificationStore();
  const [expanded, setExpanded] = useState(false);
  const [method, setMethod] = useState<KiroAuthMethod>('builder-id');
  const [form, setForm] = useState<KiroFormState>(DEFAULT_FORM);
  const [loading, setLoading] = useState(false);
  const [status, setStatus] = useState<'idle' | 'waiting' | 'success' | 'error'>('idle');
  const [error, setError] = useState('');
  const [authorizationURL, setAuthorizationURL] = useState('');
  const [userCode, setUserCode] = useState('');
  const pollingTimer = useRef<number | null>(null);

  const stopPolling = useCallback(() => {
    if (pollingTimer.current !== null) {
      window.clearInterval(pollingTimer.current);
      pollingTimer.current = null;
    }
  }, []);

  useEffect(() => stopPolling, [stopPolling]);

  const updateForm = <K extends keyof KiroFormState>(key: K, value: KiroFormState[K]) => {
    setForm((current) => ({ ...current, [key]: value }));
  };

  const finish = useCallback(() => {
    stopPolling();
    notifyAuthFilesChanged();
    setLoading(false);
    setStatus('success');
    setError('');
    setAuthorizationURL('');
    setUserCode('');
    showNotification(t('auth_login.kiro_success'), 'success');
  }, [showNotification, stopPolling, t]);

  const startPolling = useCallback(
    (state: string) => {
      stopPolling();
      pollingTimer.current = window.setInterval(async () => {
        try {
          const result = await oauthApi.getAuthStatus(state);
          if (result.status === 'ok') {
            finish();
          } else if (result.status === 'error') {
            stopPolling();
            setLoading(false);
            setStatus('error');
            setError(result.error || t('auth_login.kiro_unknown_error'));
          }
        } catch (pollError: unknown) {
          stopPolling();
          setLoading(false);
          setStatus('error');
          setError(getErrorMessage(pollError));
        }
      }, 3000);
    },
    [finish, stopPolling, t]
  );

  const buildRequest = (state: string): KiroConnectRequest => {
    const common = { state, method, region: form.region.trim() || 'us-east-1' };
    switch (method) {
      case 'idc':
        return { ...common, start_url: form.startURL.trim() };
      case 'api_key':
        return { ...common, api_key: form.apiKey.trim() };
      case 'refresh_token':
        return {
          ...common,
          refresh_auth_method: form.refreshAuthMethod,
          refresh_token: form.refreshToken.trim(),
          client_id: form.clientID.trim(),
          client_secret: form.clientSecret.trim(),
          start_url: form.refreshAuthMethod === 'idc' ? form.startURL.trim() : undefined,
        };
      case 'external_idp':
        return { ...common, credential_json: form.credentialJSON.trim() };
      default:
        return common;
    }
  };

  const connect = async () => {
    stopPolling();
    setLoading(true);
    setStatus('waiting');
    setError('');
    setAuthorizationURL('');
    setUserCode('');
    try {
      const session = await oauthApi.startAuth('kiro');
      if (!session.state) throw new Error(t('auth_login.missing_state'));
      const result = await kiroApi.connect(buildRequest(session.state));
      startPolling(session.state);
      if (result.status === 'connected') return;
      setAuthorizationURL(result.url || '');
      setUserCode(result.user_code || '');
      setLoading(false);
    } catch (connectError: unknown) {
      stopPolling();
      setLoading(false);
      setStatus('error');
      setError(getErrorMessage(connectError));
    }
  };

  const chooseMethod = (next: KiroAuthMethod) => {
    stopPolling();
    setMethod(next);
    setStatus('idle');
    setError('');
    setAuthorizationURL('');
    setUserCode('');
  };

  return (
    <Card
      title={
        <span className={styles.title}>
          {icon ? (
            <img src={icon} alt="" className={styles.icon} />
          ) : (
            <span className={styles.iconFallback} aria-hidden="true">
              <IconPlug size={18} />
            </span>
          )}
          <span>{t('auth_login.kiro_title', { name: title })}</span>
        </span>
      }
      extra={
        <Button variant={expanded ? 'secondary' : 'primary'} onClick={() => setExpanded(!expanded)}>
          {t(expanded ? 'auth_login.kiro_close' : 'auth_login.kiro_manage')}
        </Button>
      }
    >
      <p className={styles.hint}>{t('auth_login.kiro_hint')}</p>
      {expanded && (
        <div className={styles.panel}>
          <div
            className={styles.methods}
            role="tablist"
            aria-label={t('auth_login.kiro_method_label')}
          >
            {METHODS.map((item) => (
              <button
                key={item}
                type="button"
                role="tab"
                aria-selected={method === item}
                className={`${styles.method} ${method === item ? styles.activeMethod : ''}`.trim()}
                onClick={() => chooseMethod(item)}
              >
                {t(`auth_login.kiro_method_${item}`)}
              </button>
            ))}
          </div>

          <div className={styles.form} role="tabpanel">
            <p className={styles.methodHint}>{t(`auth_login.kiro_hint_${method}`)}</p>
            {method === 'idc' && (
              <Input
                label={t('auth_login.kiro_start_url')}
                value={form.startURL}
                onChange={(event) => updateForm('startURL', event.target.value)}
                placeholder="https://company.awsapps.com/start"
              />
            )}
            {(method === 'idc' || method === 'api_key' || method === 'refresh_token') && (
              <Input
                label={t('auth_login.kiro_region')}
                value={form.region}
                onChange={(event) => updateForm('region', event.target.value)}
                placeholder="us-east-1"
              />
            )}
            {method === 'api_key' && (
              <Input
                type="password"
                autoComplete="off"
                label={t('auth_login.kiro_api_key')}
                value={form.apiKey}
                onChange={(event) => updateForm('apiKey', event.target.value)}
              />
            )}
            {method === 'refresh_token' && (
              <>
                <label className={styles.fieldLabel}>
                  {t('auth_login.kiro_refresh_type')}
                  <select
                    className={styles.select}
                    value={form.refreshAuthMethod}
                    onChange={(event) =>
                      updateForm(
                        'refreshAuthMethod',
                        event.target.value === 'idc' ? 'idc' : 'builder-id'
                      )
                    }
                  >
                    <option value="builder-id">AWS Builder ID</option>
                    <option value="idc">AWS IAM Identity Center</option>
                  </select>
                </label>
                <Input
                  type="password"
                  autoComplete="off"
                  label={t('auth_login.kiro_refresh_token')}
                  value={form.refreshToken}
                  onChange={(event) => updateForm('refreshToken', event.target.value)}
                />
                <Input
                  label={t('auth_login.kiro_client_id')}
                  value={form.clientID}
                  onChange={(event) => updateForm('clientID', event.target.value)}
                />
                <Input
                  type="password"
                  autoComplete="off"
                  label={t('auth_login.kiro_client_secret')}
                  value={form.clientSecret}
                  onChange={(event) => updateForm('clientSecret', event.target.value)}
                />
                {form.refreshAuthMethod === 'idc' && (
                  <Input
                    label={t('auth_login.kiro_start_url')}
                    value={form.startURL}
                    onChange={(event) => updateForm('startURL', event.target.value)}
                    placeholder="https://company.awsapps.com/start"
                  />
                )}
              </>
            )}
            {method === 'external_idp' && (
              <label className={styles.fieldLabel}>
                {t('auth_login.kiro_external_json')}
                <textarea
                  className={styles.textarea}
                  rows={9}
                  maxLength={65536}
                  spellCheck={false}
                  value={form.credentialJSON}
                  onChange={(event) => updateForm('credentialJSON', event.target.value)}
                />
              </label>
            )}

            <div className={styles.actions}>
              <Button onClick={connect} loading={loading}>
                {t(
                  method === 'builder-id' || method === 'idc'
                    ? 'auth_login.kiro_authorize'
                    : 'auth_login.kiro_add'
                )}
              </Button>
            </div>

            {authorizationURL && (
              <div className={styles.authorization}>
                {userCode && (
                  <p>
                    {t('auth_login.kiro_user_code')}: <strong>{userCode}</strong>
                  </p>
                )}
                <Button
                  onClick={() => window.open(authorizationURL, '_blank', 'noopener,noreferrer')}
                >
                  {t('auth_login.kiro_open_aws')}
                </Button>
                <span className="status-badge">{t('auth_login.kiro_waiting')}</span>
              </div>
            )}
            {status === 'waiting' && !authorizationURL && (
              <span className="status-badge">{t('auth_login.kiro_processing')}</span>
            )}
            {status === 'error' && <div className="error-box">{error}</div>}
            {status === 'success' && (
              <div className={styles.success}>
                <span className="status-badge success">{t('auth_login.kiro_success')}</span>
                <Button variant="secondary" size="sm" onClick={() => navigate('/auth-files')}>
                  {t('auth_login.view_auth_files')}
                </Button>
              </div>
            )}
          </div>
        </div>
      )}
    </Card>
  );
}
