import { useCallback, useEffect, useState } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { useNotificationStore } from '@/stores';
import { useUsageServiceStore } from '@/stores';
import { useAuthStore } from '@/stores';
import { alertConfigApi, type AlertSMTPConfig } from '@/services/api/alertConfig';
import styles from './AlertConfigPage.module.scss';

export function AlertConfigPage() {
  const { showNotification } = useNotificationStore();
  const usageServiceBase = useUsageServiceStore((state) => state.serviceBase);
  const managementKey = useAuthStore((state) => state.managementKey);

  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [smtpHost, setSmtpHost] = useState('');
  const [smtpPort, setSmtpPort] = useState('587');
  const [smtpUsername, setSmtpUsername] = useState('');
  const [smtpPassword, setSmtpPassword] = useState('');
  const [smtpFrom, setSmtpFrom] = useState('');
  const [smtpFromName, setSmtpFromName] = useState('');
  const [alertEnabled, setAlertEnabled] = useState(false);
  const [thresholdCents, setThresholdCents] = useState('5000');
  const [checkIntervalMs, setCheckIntervalMs] = useState('60000');
  const [poolCheckInterval, setPoolCheckInterval] = useState('5');
  const [poolCheckEnabled, setPoolCheckEnabled] = useState(false);

  const loadConfig = useCallback(async () => {
    if (!usageServiceBase) return;
    setLoading(true);
    try {
      const data = await alertConfigApi.getConfig(usageServiceBase, managementKey);
      setSmtpHost(data.smtpHost || '');
      setSmtpPort(String(data.smtpPort || 587));
      setSmtpUsername(data.smtpUsername || '');
      setSmtpPassword(data.smtpPassword === '******' ? '' : data.smtpPassword || '');
      setSmtpFrom(data.smtpFrom || '');
      setSmtpFromName(data.smtpFromName || '');
      setAlertEnabled(data.alertEnabled || false);
      setThresholdCents(String(data.thresholdCents || 5000));
      setCheckIntervalMs(String(data.checkIntervalMs || 60000));
      setPoolCheckInterval(String(data.poolCheckInterval || 5));
      setPoolCheckEnabled(data.poolCheckEnabled || false);
    } catch (err: unknown) {
      showNotification(err instanceof Error ? err.message : 'Failed to load config', 'error');
    } finally {
      setLoading(false);
    }
  }, [usageServiceBase, managementKey, showNotification]);

  useEffect(() => {
    void loadConfig();
  }, [loadConfig]);

  const handleSave = async () => {
    if (!usageServiceBase) return;
    setSaving(true);
    try {
      const cfg: AlertSMTPConfig = {
        smtpHost,
        smtpPort: parseInt(smtpPort, 10) || 0,
        smtpUsername,
        smtpPassword,
        smtpFrom,
        smtpFromName,
        alertEnabled,
        thresholdCents: parseInt(thresholdCents, 10) || 5000,
        checkIntervalMs: parseInt(checkIntervalMs, 10) || 60000,
        poolCheckEnabled,
        poolCheckInterval: parseInt(poolCheckInterval, 10) || 5,
      };
      await alertConfigApi.updateConfig(usageServiceBase, cfg, managementKey);
      showNotification('告警配置已保存', 'success');
      void loadConfig();
    } catch (err: unknown) {
      showNotification(err instanceof Error ? err.message : '保存失败', 'error');
    } finally {
      setSaving(false);
    }
  };

  if (!usageServiceBase) {
    return (
      <div className={styles.container}>
        <Card className={styles.card}>
          <p className={styles.hint}>请先配置 Usage Service 连接</p>
        </Card>
      </div>
    );
  }

  return (
    <div className={styles.container}>
      <h1 className={styles.title}>告警配置</h1>
      {loading ? (
        <Card className={styles.card}><p>加载中...</p></Card>
      ) : (
        <Card className={styles.card}>
          <section className={styles.section}>
            <h3>SMTP 邮件配置</h3>
            <div className={styles.grid}>
              <label className={styles.field}>
                <span>SMTP 服务器</span>
                <Input value={smtpHost} onChange={(e) => setSmtpHost(e.target.value)} placeholder="smtp.example.com" />
              </label>
              <label className={styles.field}>
                <span>端口</span>
                <Input value={smtpPort} onChange={(e) => setSmtpPort(e.target.value)} placeholder="587" />
              </label>
              <label className={styles.field}>
                <span>用户名</span>
                <Input value={smtpUsername} onChange={(e) => setSmtpUsername(e.target.value)} />
              </label>
              <label className={styles.field}>
                <span>密码</span>
                <Input type="password" value={smtpPassword} onChange={(e) => setSmtpPassword(e.target.value)} placeholder="留空则不修改" />
              </label>
              <label className={styles.field}>
                <span>发件人地址</span>
                <Input value={smtpFrom} onChange={(e) => setSmtpFrom(e.target.value)} placeholder="alert@example.com" />
              </label>
              <label className={styles.field}>
                <span>发件人名称</span>
                <Input value={smtpFromName} onChange={(e) => setSmtpFromName(e.target.value)} placeholder="API 额度告警" />
              </label>
            </div>
          </section>

          <section className={styles.section}>
            <h3>消费阈值告警</h3>
            <label className={styles.toggle}>
              <ToggleSwitch checked={alertEnabled} onChange={setAlertEnabled} />
              <span>启用每日消费告警</span>
            </label>
            <div className={styles.grid}>
              <label className={styles.field}>
                <span>阈值（美分，5000 = $50）</span>
                <Input value={thresholdCents} onChange={(e) => setThresholdCents(e.target.value)} />
              </label>
              <label className={styles.field}>
                <span>检查间隔（毫秒）</span>
                <Input value={checkIntervalMs} onChange={(e) => setCheckIntervalMs(e.target.value)} />
              </label>
            </div>
          </section>

          <section className={styles.section}>
            <h3>池配额耗尽告警</h3>
            <label className={styles.toggle}>
              <ToggleSwitch checked={poolCheckEnabled} onChange={setPoolCheckEnabled} />
              <span>池中所有账号 5h/周额度耗尽时通知全部用户</span>
            </label>
            <div className={styles.grid}>
              <label className={styles.field}>
                <span>检查间隔（分钟）</span>
                <Input value={poolCheckInterval} onChange={(e) => setPoolCheckInterval(e.target.value)} placeholder="5" />
              </label>
            </div>
          </section>

          <div className={styles.actions}>
            <Button onClick={handleSave} disabled={saving}>
              {saving ? '保存中...' : '保存配置'}
            </Button>
          </div>
        </Card>
      )}
    </div>
  );
}
