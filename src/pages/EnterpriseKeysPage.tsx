import { useCallback, useEffect, useMemo, useState, type ChangeEvent } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Select } from '@/components/ui/Select';
import { IconDownload, IconRefreshCw, IconTrash2 } from '@/components/ui/icons';
import { useEnterpriseKeyStore, useNotificationStore } from '@/stores';
import { quotaLimitsApi, type SpendLimitEntry } from '@/services/api/quotaLimits';
import { quotaPauseApi } from '@/services/api/quotaPause';
import { UNGROUPED_DEPARTMENT_ID, type EnterpriseDepartment, type EnterpriseKeyBinding, type KeyGenPreviewItem } from '@/types';
import { downloadBlob } from '@/utils/download';
import { quotaKeyHash } from '@/utils/apiKeyHash';
import styles from './EnterpriseKeysPage.module.scss';

type ImportResultSummary = {
  totalRows: number;
  passedRows: number;
  warningRows: number;
  errorRows: number;
} | null;

type KeyActionTarget = {
  label: string;
  keyHashes: string[];
  pauseKeys: string[];
};

const DEFAULT_PAUSE_REASON = '企业 Key 管理手动停用';

const normalizeQuotaKeyHash = (value: string): string => {
  const normalized = value.trim().toLowerCase();
  return normalized.length === 64 ? normalized.slice(0, 8) : normalized;
};

const nowMs = () => Date.now();

const formatErrorDetails = (raw?: string) => {
  if (!raw) return '';
  try {
    const parsed = JSON.parse(raw);
    return JSON.stringify(parsed, null, 2);
  } catch {
    return raw;
  }
};

export function EnterpriseKeysPage() {
  const {
    departments,
    keyBindings,
    importHistory,
    loading,
    error,
    selectedDepartmentId,
    selectedApiKeys,
    setSelectedDepartmentId,
    setSelectedApiKeys,
    clearSelection,
    fetchDepartments,
    upsertDepartments,
    deleteDepartment,
    fetchKeyBindings,
    createKeyBinding,
    updateKeyBinding,
    deleteKeyBinding,
    deleteKeyBindings,
    generatePreview,
    importKeys,
    fetchImportHistory,
  } = useEnterpriseKeyStore();
  const { showNotification, showConfirmation } = useNotificationStore();
  const { t } = useTranslation();
  const navigate = useNavigate();

  const [departmentModalOpen, setDepartmentModalOpen] = useState(false);
  const [importModalOpen, setImportModalOpen] = useState(false);
  const [addKeyModalOpen, setAddKeyModalOpen] = useState(false);
  const [pauseModalOpen, setPauseModalOpen] = useState(false);
  const [quotaModalOpen, setQuotaModalOpen] = useState(false);

  const [searchQuery, setSearchQuery] = useState('');
  const [pausedKeyHashes, setPausedKeyHashes] = useState<Set<string>>(new Set());
  const [quotaOverrides, setQuotaOverrides] = useState<SpendLimitEntry[]>([]);
  const [quotaEnabled, setQuotaEnabled] = useState(true);
  const [actionTarget, setActionTarget] = useState<KeyActionTarget | null>(null);
  const [pauseReason, setPauseReason] = useState(DEFAULT_PAUSE_REASON);
  const [pauseDurationSec, setPauseDurationSec] = useState('3600');
  const [quotaDailyCents, setQuotaDailyCents] = useState('');
  const [quotaWeeklyCents, setQuotaWeeklyCents] = useState('');
  const [actionSaving, setActionSaving] = useState(false);

  const [editingDepartments, setEditingDepartments] = useState<EnterpriseDepartment[]>([]);
  const [newDepartmentName, setNewDepartmentName] = useState('');
  const [newDepartmentPrefix, setNewDepartmentPrefix] = useState('');

  const [newKeyUserName, setNewKeyUserName] = useState('');
  const [newKeyEmail, setNewKeyEmail] = useState('');
  const [newKeyDepartmentId, setNewKeyDepartmentId] = useState('');
  const [newKeyApiKey, setNewKeyApiKey] = useState('');
  const [editModalOpen, setEditModalOpen] = useState(false);
  const [editingApiKey, setEditingApiKey] = useState('');
  const [editingUserName, setEditingUserName] = useState('');
  const [editingEmail, setEditingEmail] = useState('');
  const [editingDepartmentId, setEditingDepartmentId] = useState('');

  const [previewItems, setPreviewItems] = useState<KeyGenPreviewItem[]>([]);
  const [importFileName, setImportFileName] = useState('');
  const [importSummary, setImportSummary] = useState<ImportResultSummary>(null);

  const loadQuotaState = useCallback(async () => {
    const [paused, quota] = await Promise.all([quotaPauseApi.listPaused(), quotaLimitsApi.getConfig()]);
    setQuotaEnabled(!!quota.enabled);
    setPausedKeyHashes(new Set((paused.entries ?? []).map((entry) => normalizeQuotaKeyHash(entry.key_hash))));
    setQuotaOverrides(
      (quota.overrides ?? [])
        .filter((entry) => entry.apply_to === 'api-key')
        .map((entry) => ({ ...entry, apply_value: normalizeQuotaKeyHash(entry.apply_value) }))
    );
  }, []);

  useEffect(() => {
    void Promise.all([fetchDepartments(), fetchKeyBindings(), fetchImportHistory(20), loadQuotaState()]).catch(() => {});
  }, [fetchDepartments, fetchImportHistory, fetchKeyBindings, loadQuotaState]);

  const departmentRows = useMemo(() => {
    if (selectedDepartmentId === 'all') {
      return keyBindings;
    }
    if (selectedDepartmentId === UNGROUPED_DEPARTMENT_ID) {
      return keyBindings.filter((item) => !item.departmentId);
    }
    return keyBindings.filter((item) => item.departmentId === selectedDepartmentId);
  }, [keyBindings, selectedDepartmentId]);

  const filteredRows = useMemo(() => {
    const query = searchQuery.trim().toLowerCase();
    if (!query) return departmentRows;
    return departmentRows.filter((item) =>
      [item.userName, item.email, item.apiKeyHash, item.apiKey]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(query))
    );
  }, [departmentRows, searchQuery]);

  const managedDepartments = useMemo(
    () => departments.filter((item) => item.id !== UNGROUPED_DEPARTMENT_ID && item.name !== '未分组'),
    [departments]
  );

  const departmentOptions = useMemo(
    () => [
      { value: 'all', label: '全部部门' },
      { value: UNGROUPED_DEPARTMENT_ID, label: '未分组' },
      ...managedDepartments.map((item) => ({ value: item.id, label: item.name })),
    ],
    [managedDepartments]
  );

  const selectableRows = filteredRows.filter((row) => row.apiKey);
  const allSelected = selectableRows.length > 0 && selectableRows.every((row) => selectedApiKeys.includes(row.apiKey));

  const departmentNameMap = useMemo(() => {
    const map = new Map<string, string>();
    managedDepartments.forEach((d) => map.set(d.id, d.name));
    return map;
  }, [managedDepartments]);

  const selectedActionRows = useMemo(
    () => filteredRows.filter((row) => selectedApiKeys.includes(row.apiKey) && row.apiKeyHash),
    [filteredRows, selectedApiKeys]
  );


  const keyHashesFromRows = (rows: EnterpriseKeyBinding[]) =>
    Array.from(new Set(rows.map((row) => (row.apiKey ? quotaKeyHash(row.apiKey) : row.apiKeyHash.slice(0, 8).toLowerCase())).filter(Boolean)));

  const pauseKeysFromRows = (rows: EnterpriseKeyBinding[]) =>
    Array.from(new Set(rows.filter((row) => row.apiKey).map((row) => row.apiKey.trim())));

  const openPauseTarget = (target: KeyActionTarget) => {
    setActionTarget(target);
    setPauseReason(DEFAULT_PAUSE_REASON);
    setPauseDurationSec('3600');
    setPauseModalOpen(true);
  };

  const openQuotaTarget = (target: KeyActionTarget) => {
    const existing = quotaOverrides.find((entry) => target.keyHashes.includes(entry.apply_value.toLowerCase()));
    setActionTarget(target);
    setQuotaDailyCents(existing ? String(existing.daily_cents) : '');
    setQuotaWeeklyCents(existing ? String(existing.weekly_cents) : '');
    setQuotaModalOpen(true);
  };

  const requireSelectedTarget = (): KeyActionTarget | null => {
    const keyHashes = keyHashesFromRows(selectedActionRows);
    const pauseKeys = pauseKeysFromRows(selectedActionRows);
    if (keyHashes.length === 0 || pauseKeys.length === 0) {
      showNotification('请先选择需要操作的 Key', 'error');
      return null;
    }
    return { label: `选中的 ${keyHashes.length} 个 Key`, keyHashes, pauseKeys };
  };


  const refreshAll = async () => {
    try {
      await Promise.all([fetchDepartments(), fetchKeyBindings(), fetchImportHistory(20), loadQuotaState()]);
      clearSelection();
      showNotification('企业 Key 数据已刷新', 'success');
    } catch {
      showNotification('刷新失败，请稍后重试', 'error');
    }
  };

  const toggleAllSelection = (checked: boolean) => {
    if (!checked) {
      setSelectedApiKeys([]);
      return;
    }
    setSelectedApiKeys(selectableRows.map((row) => row.apiKey));
  };

  const toggleRowSelection = (apiKey: string, checked: boolean) => {
    if (!checked) {
      setSelectedApiKeys(selectedApiKeys.filter((item) => item !== apiKey));
      return;
    }
    if (selectedApiKeys.includes(apiKey)) return;
    setSelectedApiKeys([...selectedApiKeys, apiKey]);
  };

  const handleSaveDepartments = async () => {
    try {
      await upsertDepartments(editingDepartments);
      setDepartmentModalOpen(false);
      showNotification('部门信息已保存', 'success');
    } catch {
      showNotification('保存部门失败', 'error');
    }
  };

  const handleAddDepartmentRow = () => {
    const name = newDepartmentName.trim();
    const prefix = newDepartmentPrefix.trim().toLowerCase();
    if (!name || !prefix) {
      showNotification('请输入部门名称和前缀', 'error');
      return;
    }
    const id = `${prefix}-${Math.random().toString(36).slice(2, 8)}`;
    const timestamp = nowMs();
    setEditingDepartments([
      ...editingDepartments,
      {
        id,
        name,
        prefix,
        sortOrder: editingDepartments.length,
        enabled: true,
        system: false,
        createdAtMs: timestamp,
        updatedAtMs: timestamp,
      },
    ]);
    setNewDepartmentName('');
    setNewDepartmentPrefix('');
  };

  const handleDeleteDepartment = async (id: string) => {
    showConfirmation({
      title: '删除部门',
      message: '删除后，该部门关联的 Key 将转为未分组，确认继续吗？',
      variant: 'danger',
      confirmText: '确认删除',
      onConfirm: async () => {
        const existsInServer = managedDepartments.some((item) => item.id === id);
        if (!existsInServer) {
          setEditingDepartments((prev) => prev.filter((item) => item.id !== id));
          showNotification('部门已移除（待保存）', 'success');
          return;
        }
        try {
          await deleteDepartment(id);
          setEditingDepartments((prev) => prev.filter((item) => item.id !== id));
          showNotification('部门已删除', 'success');
        } catch {
          showNotification('删除部门失败', 'error');
        }
      },
    });
  };

  const handleCreateKey = async () => {
    const userName = newKeyUserName.trim();
    const email = newKeyEmail.trim();
    const customApiKey = newKeyApiKey.trim();
    if (!userName || !newKeyDepartmentId) {
      showNotification('请填写用户名并选择部门', 'error');
      return;
    }
    try {
      await createKeyBinding(userName, newKeyDepartmentId, customApiKey || undefined, email || undefined);
      setAddKeyModalOpen(false);
      setNewKeyUserName('');
      setNewKeyEmail('');
      setNewKeyApiKey('');
      clearSelection();
      showNotification('Key 已创建', 'success');
    } catch {
      showNotification('创建 Key 失败', 'error');
    }
  };

  const handleOpenEditKey = (apiKey: string, userName: string, departmentId: string, email?: string) => {
    setEditingApiKey(apiKey);
    setEditingUserName(userName);
    setEditingEmail(email ?? '');
    setEditingDepartmentId(departmentId);
    setEditModalOpen(true);
  };

  const handleConfirmEditKey = async () => {
    const userName = editingUserName.trim();
    const email = editingEmail.trim();
    if (!editingApiKey || !userName || !editingDepartmentId) {
      showNotification('请填写用户名并选择部门', 'error');
      return;
    }
    try {
      await updateKeyBinding(editingApiKey, userName, editingDepartmentId, email || undefined);
      setEditModalOpen(false);
      showNotification('Key 信息已更新', 'success');
    } catch {
      showNotification('更新 Key 失败', 'error');
    }
  };

  const handleDeleteKey = async (apiKey: string) => {
    showConfirmation({
      title: '删除 Key',
      message: '确认删除该 API Key 吗？',
      variant: 'danger',
      confirmText: '确认删除',
      onConfirm: async () => {
        try {
          await deleteKeyBinding(apiKey);
          showNotification('Key 已删除', 'success');
        } catch {
          showNotification('删除 Key 失败', 'error');
        }
      },
    });
  };

  const handleBatchDeleteKeys = () => {
    if (selectedApiKeys.length === 0) {
      showNotification('请先选择需要删除的记录', 'error');
      return;
    }
    showConfirmation({
      title: '批量删除 Key',
      message: `确认删除选中的 ${selectedApiKeys.length} 个 API Key 吗？`,
      variant: 'danger',
      confirmText: '确认删除',
      onConfirm: async () => {
        try {
          await deleteKeyBindings(selectedApiKeys);
          showNotification(`已删除 ${selectedApiKeys.length} 个 Key`, 'success');
        } catch {
          showNotification('批量删除失败', 'error');
        }
      },
    });
  };

  const handleImportFileChange = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) return;
    try {
      const text = await file.text();
      const response = await generatePreview(text);
      setPreviewItems(response);
      setImportFileName(file.name);
      setImportSummary(null);
      showNotification(`预览已生成：${response.length} 行`, 'success');
    } catch {
      showNotification('CSV 预览生成失败', 'error');
    }
  };

  const handleConfirmImport = async () => {
    const okItems = previewItems.filter((item) => item.status === 'ok');
    try {
      await importKeys(okItems);
      const summary = {
        totalRows: previewItems.length,
        passedRows: okItems.length,
        warningRows: previewItems.filter((item) => item.status === 'warning').length,
        errorRows: previewItems.filter((item) => item.status === 'error').length,
      };
      setImportSummary(summary);
      showNotification('导入已提交并刷新列表', 'success');
    } catch {
      showNotification('导入失败', 'error');
    }
  };

  const handleExportSelected = () => {
    const selectedRows = filteredRows.filter((row) => selectedApiKeys.includes(row.apiKey));
    if (selectedRows.length === 0) {
      showNotification('请先选择需要导出的记录', 'error');
      return;
    }

    const header = '用户名,邮箱,API Key,部门';
    const body = selectedRows
      .map((item) => {
        const departmentName = item.departmentId
          ? (departmentNameMap.get(item.departmentId) ?? item.departmentId)
          : '未分组';
        return [item.userName, item.email || '', item.apiKey || '', departmentName]
          .map((field) => `"${String(field).split('"').join('""')}"`)
          .join(',');
      })
      .join('\n');

    const csv = `${header}\n${body}`;
    downloadBlob({
      filename: `enterprise-keys-${Date.now()}.csv`,
      blob: new Blob([csv], { type: 'text/csv;charset=utf-8;' }),
      withBom: true,
    });
    showNotification(`已导出 ${selectedRows.length} 条记录`, 'success');
  };

  const handlePauseTarget = async () => {
    if (!actionTarget) return;
    setActionSaving(true);
    try {
      const secs = parseInt(pauseDurationSec, 10) || 0;
      await Promise.all(
        actionTarget.pauseKeys.map((apiKey) =>
          quotaPauseApi.pauseKey(apiKey, pauseReason.trim() || DEFAULT_PAUSE_REASON, secs > 0 ? secs : undefined)
        )
      );
      setPauseModalOpen(false);
      setActionTarget(null);
      showNotification(`已停用 ${actionTarget.pauseKeys.length} 个 Key`, 'success');
    } catch (err) {
      showNotification(`停用失败：${err instanceof Error ? err.message : String(err)}`, 'error');
    } finally {
      setActionSaving(false);
      loadQuotaState().catch(() => {});
    }
  };

  const handleResumeTarget = async (target: KeyActionTarget) => {
    setActionSaving(true);
    try {
      await Promise.all(target.pauseKeys.map((apiKey) => quotaPauseApi.resumeKey(apiKey)));
      showNotification(`已恢复 ${target.keyHashes.length} 个 Key`, 'success');
    } catch (err) {
      showNotification(`恢复失败：${err instanceof Error ? err.message : String(err)}`, 'error');
    } finally {
      setActionSaving(false);
      loadQuotaState().catch(() => {});
    }
  };

  const handleSaveQuotaTarget = async () => {
    if (!actionTarget) return;
    setActionSaving(true);
    try {
      const daily = Math.round(parseFloat(quotaDailyCents) || 0);
      const weekly = Math.round(parseFloat(quotaWeeklyCents) || 0);
      const config = await quotaLimitsApi.getConfig();
      const targetSet = new Set(actionTarget.keyHashes);
      const preserved = (config.overrides ?? []).filter(
        (entry) => entry.apply_to !== 'api-key' || !targetSet.has(normalizeQuotaKeyHash(entry.apply_value))
      );
      const nextOverrides = [
        ...preserved,
        ...actionTarget.keyHashes.map((keyHash) => ({
          apply_to: 'api-key',
          apply_value: keyHash,
          daily_cents: daily,
          weekly_cents: weekly,
        })),
      ];
      await quotaLimitsApi.updateConfig({ overrides: nextOverrides });

      await loadQuotaState();
      setQuotaModalOpen(false);
      setActionTarget(null);
      showNotification(`已设置 ${actionTarget.keyHashes.length} 个 Key 的限额`, 'success');
    } catch (err) {
      showNotification(`限额保存失败：${err instanceof Error ? err.message : String(err)}`, 'error');
    } finally {
      setActionSaving(false);
    }
  };

  return (
    <div className={styles.container}>
      <h1 className={styles.title}>企业 Key 管理</h1>

      <Card
        title="Key 列表"
        extra={
          <div className={styles.toolbar}>
            <Select
              value={selectedDepartmentId}
              options={departmentOptions}
              onChange={(value) => {
                setSelectedDepartmentId(value);
                clearSelection();
              }}
              ariaLabel="部门筛选"
            />
            <Input
              className={styles.searchInput}
              value={searchQuery}
              onChange={(e) => {
                setSearchQuery(e.target.value);
                clearSelection();
              }}
              placeholder="搜索用户、邮箱或 Key"
              aria-label="搜索用户"
            />
            <Button
              variant="secondary"
              onClick={() => {
                setEditingDepartments(managedDepartments);
                setDepartmentModalOpen(true);
              }}
            >
              部门管理
            </Button>
            <Button variant="secondary" onClick={() => setImportModalOpen(true)}>
              导入 CSV
            </Button>
            <Button
              variant="secondary"
              onClick={() => {
                if (!newKeyDepartmentId && managedDepartments.length > 0) {
                  setNewKeyDepartmentId(managedDepartments[0].id);
                }
                setAddKeyModalOpen(true);
              }}
            >
              新增 Key
            </Button>
            <Button variant="secondary" onClick={refreshAll}>
              <IconRefreshCw size={14} />
              刷新
            </Button>
            <Button variant="danger" onClick={handleBatchDeleteKeys} disabled={selectedApiKeys.length === 0}>
              <IconTrash2 size={14} />
              批量删除
            </Button>
            <Button onClick={handleExportSelected}>
              <IconDownload size={14} />
              导出选中
            </Button>
            <Button
              variant="danger"
              onClick={() => {
                const target = requireSelectedTarget();
                if (target) openPauseTarget(target);
              }}
              disabled={selectedActionRows.length === 0 || actionSaving}
            >
              批量停用
            </Button>
            <Button
              variant="secondary"
              onClick={() => {
                const target = requireSelectedTarget();
                if (target) openQuotaTarget(target);
              }}
              disabled={selectedActionRows.length === 0 || actionSaving}
            >
              批量限额
            </Button>
            <Button variant="secondary" onClick={() => navigate('/quota-limits')}>
              限额管理
            </Button>
            <span className={styles.quotaBadge} data-enabled={quotaEnabled}>
              Quota: {quotaEnabled ? '已启用' : '未启用'}
            </span>
            {!quotaEnabled && (
              <Button
                variant="secondary"
                size="sm"
                disabled={actionSaving}
                onClick={async () => {
                  setActionSaving(true);
                  try {
                    const cur = await quotaLimitsApi.getConfig();
                    await quotaLimitsApi.updateConfig({
                      enabled: true,
                      default: cur.default,
                      overrides: cur.overrides ?? [],
                    });
                    await loadQuotaState();
                    showNotification('Quota 已启用', 'success');
                  } catch (err) {
                    showNotification(`启用 Quota 失败：${err instanceof Error ? err.message : String(err)}`, 'error');
                  } finally {
                    setActionSaving(false);
                  }
                }}
              >
                启用 Quota
              </Button>
            )}
          </div>
        }
      >
        {error && <div className="error-box">{error}</div>}
        <div className={styles.tableWrapper}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>
                  <input
                    type="checkbox"
                    checked={allSelected}
                    onChange={(e) => toggleAllSelection(e.target.checked)}
                    aria-label="全选"
                  />
                </th>
                <th>用户名</th>
                <th>{t('enterpriseKeys.email')}</th>
                <th>API Key</th>
                <th>部门</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {filteredRows.map((item) => {
                const checked = selectedApiKeys.includes(item.apiKey);
                const keyHash = item.apiKey
                  ? quotaKeyHash(item.apiKey)
                  : normalizeQuotaKeyHash(item.apiKeyHash);
                const pauseKeyHash = keyHash;
                const keyTarget = { label: item.userName || keyHash, keyHashes: [keyHash], pauseKeys: [item.apiKey] };
                const rowKey = `${item.apiKey || ''}|${item.userName}|${item.departmentId || ''}`;
                return (
                  <tr key={rowKey}>
                    <td>
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={(e) => toggleRowSelection(item.apiKey, e.target.checked)}
                        aria-label={`选择 ${item.userName}`}
                      />
                    </td>
                    <td>{item.userName}</td>
                    <td>{item.email || '-'}</td>
                    <td className={styles.mono}>{item.apiKey || '-'}</td>
                    <td>{item.departmentId ? (departmentNameMap.get(item.departmentId) ?? item.departmentId) : '未分组'}</td>
                    <td>
                      {item.apiKey ? (
                        <div className={styles.rowActions}>
                          {pauseKeyHash && pausedKeyHashes.has(pauseKeyHash) ? (
                            <Button
                              variant="secondary"
                              size="sm"
                              disabled={actionSaving}
                              onClick={() => handleResumeTarget(keyTarget)}
                            >
                              恢复
                            </Button>
                          ) : (
                            <Button
                              variant="secondary"
                              size="sm"
                              disabled={!item.apiKey || actionSaving}
                              onClick={() => openPauseTarget(keyTarget)}
                            >
                              停用
                            </Button>
                          )}
                          <Button
                            variant="secondary"
                            size="sm"
                            disabled={!keyHash || actionSaving}
                            onClick={() => openQuotaTarget(keyTarget)}
                          >
                            限额
                          </Button>
                          <Button
                            variant="secondary"
                            size="sm"
                            onClick={() => handleOpenEditKey(item.apiKey, item.userName, item.departmentId, item.email)}
                          >
                            编辑
                          </Button>
                          <Button variant="danger" size="sm" onClick={() => handleDeleteKey(item.apiKey)}>
                            <IconTrash2 size={14} /> 删除
                          </Button>
                        </div>
                      ) : (
                        '-'
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {!loading && filteredRows.length === 0 && <div className={styles.empty}>暂无数据</div>}
        </div>
        <div className={styles.selectionMeta}>已选 {selectedApiKeys.length} 项</div>
      </Card>

      <Card title="导入历史（最近 20 条）">
        <div className={styles.historyList}>
          {importHistory.map((item) => (
            <div key={item.taskId} className={styles.historyItem}>
              <div>{item.csvFileName || '-'}</div>
              <div>总计 {item.totalRows} / 通过 {item.passedRows} / 警告 {item.warningRows} / 错误 {item.errorRows}</div>
              <div>状态：{item.status}</div>
              {item.errorDetails && (
                <details className={styles.errorDetails}>
                  <summary>错误详情</summary>
                  <pre>{formatErrorDetails(item.errorDetails)}</pre>
                </details>
              )}
            </div>
          ))}
          {importHistory.length === 0 && <div className={styles.empty}>暂无导入历史</div>}
        </div>
      </Card>

      <Modal
        open={departmentModalOpen}
        onClose={() => setDepartmentModalOpen(false)}
        title="部门管理"
        footer={
          <>
            <Button variant="secondary" onClick={() => setDepartmentModalOpen(false)}>
              取消
            </Button>
            <Button onClick={handleSaveDepartments}>保存</Button>
          </>
        }
      >
        <div className={styles.modalSection}>
          {editingDepartments.map((item, idx) => (
            <div key={item.id} className={styles.rowInputs}>
              <Input
                value={item.name}
                onChange={(e) => {
                  const next = [...editingDepartments];
                  next[idx] = { ...next[idx], name: e.target.value, updatedAtMs: nowMs() };
                  setEditingDepartments(next);
                }}
                placeholder="部门名称"
              />
              <Input
                value={item.prefix}
                onChange={(e) => {
                  const next = [...editingDepartments];
                  next[idx] = {
                    ...next[idx],
                    prefix: e.target.value.toLowerCase(),
                    updatedAtMs: nowMs(),
                  };
                  setEditingDepartments(next);
                }}
                placeholder="前缀"
              />
              <Button variant="danger" onClick={() => handleDeleteDepartment(item.id)}>
                删除
              </Button>
            </div>
          ))}
        </div>
        <div className={styles.newDepartmentSection}>
          <Input
            value={newDepartmentName}
            onChange={(e) => setNewDepartmentName(e.target.value)}
            placeholder="新部门名称"
          />
          <Input
            value={newDepartmentPrefix}
            onChange={(e) => setNewDepartmentPrefix(e.target.value)}
            placeholder="新部门前缀"
          />
          <Button variant="secondary" onClick={handleAddDepartmentRow}>
            新增部门
          </Button>
        </div>
      </Modal>

      <Modal
        open={importModalOpen}
        onClose={() => setImportModalOpen(false)}
        title="导入 CSV"
        footer={
          <>
            <Button variant="secondary" onClick={() => setImportModalOpen(false)}>
              关闭
            </Button>
            <Button onClick={handleConfirmImport} disabled={previewItems.length === 0}>
              确认导入
            </Button>
          </>
        }
      >
        <div className={styles.modalSection}>
          <Input type="file" accept=".csv,text/csv" onChange={handleImportFileChange} />
          {importFileName && <div className={styles.importFileName}>当前文件：{importFileName}</div>}
          <div className={styles.previewList}>
            {previewItems.map((item, idx) => (
              <div key={`${item.userName}-${idx}`} className={`${styles.previewItem} ${styles[`preview_${item.status}`]}`}>
                <div>{item.userName}</div>
                <div>{item.email || '-'}</div>
                <div>{item.departmentName || item.departmentId || '-'}</div>
                <div className={styles.mono}>{item.generatedKey || '-'}</div>
                <div>{item.status}{item.errorReason ? `：${item.errorReason}` : ''}</div>
              </div>
            ))}
            {previewItems.length === 0 && <div className={styles.empty}>请先选择 CSV 文件生成预览</div>}
          </div>
          {importSummary && (
            <div className={styles.importSummary}>
              导入结果：总计 {importSummary.totalRows}，通过 {importSummary.passedRows}，警告 {importSummary.warningRows}，错误 {importSummary.errorRows}
            </div>
          )}
        </div>
      </Modal>

      <Modal
        open={addKeyModalOpen}
        onClose={() => setAddKeyModalOpen(false)}
        title="新增 Key"
        footer={
          <>
            <Button variant="secondary" onClick={() => setAddKeyModalOpen(false)}>
              取消
            </Button>
            <Button onClick={handleCreateKey}>创建</Button>
          </>
        }
      >
        <div className={styles.modalSection}>
          <Input
            value={newKeyUserName}
            onChange={(e) => setNewKeyUserName(e.target.value)}
            placeholder="用户名"
          />
          <Input
            value={newKeyEmail}
            onChange={(e) => setNewKeyEmail(e.target.value)}
            placeholder={t('enterpriseKeys.emailPlaceholder')}
          />
          <Input
            value={newKeyApiKey}
            onChange={(e) => setNewKeyApiKey(e.target.value)}
            placeholder="指定 API Key（可选，不填则自动生成）"
          />
          <Select
            value={newKeyDepartmentId}
            options={managedDepartments.map((d) => ({ value: d.id, label: d.name }))}
            onChange={setNewKeyDepartmentId}
            ariaLabel="选择部门"
          />
        </div>
      </Modal>

      <Modal
        open={editModalOpen}
        onClose={() => setEditModalOpen(false)}
        title="编辑 Key"
        footer={
          <>
            <Button variant="secondary" onClick={() => setEditModalOpen(false)}>
              取消
            </Button>
            <Button onClick={handleConfirmEditKey}>保存</Button>
          </>
        }
      >
        <div className={styles.modalSection}>
          <Input value={editingApiKey} disabled placeholder="API Key" />
          <Input
            value={editingUserName}
            onChange={(e) => setEditingUserName(e.target.value)}
            placeholder="用户名"
          />
          <Input
            value={editingEmail}
            onChange={(e) => setEditingEmail(e.target.value)}
            placeholder={t('enterpriseKeys.emailPlaceholder')}
          />
          <Select
            value={editingDepartmentId}
            options={managedDepartments.map((d) => ({ value: d.id, label: d.name }))}
            onChange={setEditingDepartmentId}
            ariaLabel="选择部门"
          />
        </div>
      </Modal>

      <Modal
        open={pauseModalOpen}
        onClose={() => {
          setPauseModalOpen(false);
          setActionTarget(null);
        }}
        title="停用 Key"
        footer={
          <>
            <Button
              variant="secondary"
              disabled={actionSaving}
              onClick={() => {
                setPauseModalOpen(false);
                setActionTarget(null);
              }}
            >
              取消
            </Button>
            <Button onClick={handlePauseTarget} loading={actionSaving}>
              确认停用
            </Button>
          </>
        }
      >
        <div className={styles.modalSection}>
          <div className={styles.actionTarget}>目标：{actionTarget?.label ?? '-'}</div>
          <Input
            label="停用原因"
            value={pauseReason}
            onChange={(e) => setPauseReason(e.target.value)}
            placeholder={DEFAULT_PAUSE_REASON}
          />
          <Input
            label="停用时长（秒，0 为永久）"
            type="number"
            min="0"
            value={pauseDurationSec}
            onChange={(e) => setPauseDurationSec(e.target.value)}
          />
        </div>
      </Modal>

      <Modal
        open={quotaModalOpen}
        onClose={() => {
          setQuotaModalOpen(false);
          setActionTarget(null);
        }}
        title="设置限额"
        footer={
          <>
            <Button
              variant="secondary"
              disabled={actionSaving}
              onClick={() => {
                setQuotaModalOpen(false);
                setActionTarget(null);
              }}
            >
              取消
            </Button>
            <Button onClick={handleSaveQuotaTarget} loading={actionSaving}>
              保存限额
            </Button>
          </>
        }
      >
        <div className={styles.modalSection}>
          <div className={styles.actionTarget}>目标：{actionTarget?.label ?? '-'}</div>
          <div className={styles.fieldHint}>留空或不填表示不限额；填 0 表示立即停用；填整数（厘分），如 100 = $1</div>
          <Input
            label="每日限额（厘分）"
            type="number"
            min="0"
            step="1"
            value={quotaDailyCents}
            onChange={(e) => setQuotaDailyCents(e.target.value)}
          />
          <Input
            label="每周限额（厘分）"
            type="number"
            min="0"
            step="1"
            value={quotaWeeklyCents}
            onChange={(e) => setQuotaWeeklyCents(e.target.value)}
          />
        </div>
      </Modal>
    </div>
  );
}
