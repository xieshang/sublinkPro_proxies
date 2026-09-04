import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  Grid,
  IconButton,
  Stack,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  Typography,
  Alert,
  CircularProgress,
  FormControlLabel,
  Checkbox,
  Tooltip,
  TablePagination,
  FormControl,
  InputLabel,
  Select,
  MenuItem
} from '@mui/material';
import {
  IconPlayerPlay,
  IconPlayerStop,
  IconRefresh,
  IconTrash,
  IconDownload,
  IconClock,
  IconGauge
} from '@tabler/icons-react';
import MainCard from 'ui-component/cards/MainCard';
import {
  listGitHubCrawlConfigs,
  createGitHubCrawlConfig,
  updateGitHubCrawlConfig,
  deleteGitHubCrawlConfig,
  toggleGitHubCrawlConfig,
  runGitHubCrawlNow,
  listGitHubCrawlLogs,
  clearGitHubCrawlLogs,
  listGitHubCrawlNodes,
  clearGitHubCrawlNodes,
  deleteInvalidGitHubCrawlNodes,
  deleteGitHubCrawlNodes,
  promoteGitHubCrawlNodes,
  testGitHubCrawlNodeDelay,
  testGitHubCrawlNodeSpeed,
  testGitHubCrawlNodes,
  stopGitHubCrawl,
  listGitHubCrawlBlacklist,
  addGitHubCrawlBlacklist,
  updateGitHubCrawlBlacklist,
  deleteGitHubCrawlBlacklist
} from 'api/githubCrawl';
import { IconEdit, IconPlus } from '@tabler/icons-react';
import { getSpeedTestConfig } from 'api/nodes';

// 与后端 githubCrawlLogMaxKeep 一致：前端日志最多缓存 500 行
const GITHUB_CRAWL_LOG_MAX = 500;

// 间隔小时/分钟 → cron（后台调度仍用 5 字段 cron）
function intervalToCron(hour, minute) {
  const h = Math.max(0, Math.min(23, Number(hour) || 0));
  const m = Math.max(0, Math.min(59, Number(minute) || 0));
  if (h === 0 && m === 0) return '0 */6 * * *';
  if (h > 0 && m === 0) return `0 */${h} * * *`;
  if (h === 0 && m > 0) return `*/${m} * * * *`;
  return `${m} */${h} * * *`;
}

// cron → 间隔小时/分钟（兼容常见写法）
function cronToInterval(cronExpr) {
  const parts = String(cronExpr || '')
    .trim()
    .split(/\s+/);
  if (parts.length < 5) return { hour: 6, minute: 0 };
  const [minPart, hourPart] = parts;
  if (minPart.startsWith('*/') && (hourPart === '*' || hourPart === '')) {
    const m = parseInt(minPart.slice(2), 10);
    return { hour: 0, minute: Number.isFinite(m) ? Math.min(59, Math.max(0, m)) : 0 };
  }
  if (hourPart.startsWith('*/')) {
    const h = parseInt(hourPart.slice(2), 10);
    const m = /^\d+$/.test(minPart) ? parseInt(minPart, 10) : 0;
    return {
      hour: Number.isFinite(h) ? Math.min(23, Math.max(0, h)) : 6,
      minute: Number.isFinite(m) ? Math.min(59, Math.max(0, m)) : 0
    };
  }
  if (/^\d+$/.test(minPart) && /^\d+$/.test(hourPart)) {
    // 定点：按「每天 H:M」展示为小时/分钟，保存时仍会按间隔规则生成
    return {
      hour: Math.min(23, Math.max(0, parseInt(hourPart, 10) || 0)),
      minute: Math.min(59, Math.max(0, parseInt(minPart, 10) || 0))
    };
  }
  return { hour: 6, minute: 0 };
}

const emptyForm = {
  name: '',
  githubToken: '',
  searchKeywords: '',
  searchInterval: 3600,
  collectionInterval: 3600,
  maxCrawlLinks: 40,
  useProxy: false,
  cronExpr: '0 */6 * * *',
  enabled: false,
  group: 'github',
  remark: '',
  autoPromote: false,
  hour: 6,
  minute: 0,
  // ---- 独立节点定时全测 ----
  testEnabled: false,
  testCronExpr: '0 0 */6 * * *',
  testProfileId: 0,
  testFailureThreshold: 3,
  testAutoDeleteEnabled: false
};

export default function GitHubCrawlPage() {
  const { t } = useTranslation();
  const [configs, setConfigs] = useState([]);
  const [selectedId, setSelectedId] = useState(null);
  const [form, setForm] = useState(emptyForm);
  const [logs, setLogs] = useState([]);
  const [nodes, setNodes] = useState([]);
  const [selectedNodeIds, setSelectedNodeIds] = useState([]);
  const [filterKeyword, setFilterKeyword] = useState('');
  const [filterStatus, setFilterStatus] = useState('all'); // all|valid|invalid|untested
  const [filterPromoted, setFilterPromoted] = useState('all'); // all|promoted|unpromoted
  const [filterProtocol, setFilterProtocol] = useState('all');
  const [testTargetIds, setTestTargetIds] = useState([]); // empty = all nodes for full test
  const [, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [running, setRunning] = useState(false);
  const [promoting, setPromoting] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  const [logAfterId, setLogAfterId] = useState(0);
  const [page, setPage] = useState(0);
  const [rowsPerPage, setRowsPerPage] = useState(() => {
    const saved = Number(localStorage.getItem('github_crawl_rowsPerPage') || 20);
    return [10, 20, 50, 100, 200].includes(saved) ? saved : 20;
  });
  const [testing, setTesting] = useState(false);
  const [testDialogOpen, setTestDialogOpen] = useState(false);
  const [profiles, setProfiles] = useState([]);
  const [selectedProfileId, setSelectedProfileId] = useState('');
  const [profilesLoading, setProfilesLoading] = useState(false);
  // 定时全测策略下拉数据复用
  const [testProfilesLoading, setTestProfilesLoading] = useState(false);
  const [blacklist, setBlacklist] = useState([]);
  const [blacklistForm, setBlacklistForm] = useState({ scope: 'link', target: '', repo: '', reason: '' });
  const [blacklistDialogOpen, setBlacklistDialogOpen] = useState(false);
  const [editingBlacklistId, setEditingBlacklistId] = useState(null);
  const [blacklistSaving, setBlacklistSaving] = useState(false);

  const selected = useMemo(() => configs.find((c) => c.id === selectedId) || null, [configs, selectedId]);
  // 日志倒序展示（新日志在上）
  const displayLogs = useMemo(() => [...logs].reverse(), [logs]);
  const protocolOptions = useMemo(() => {
    const set = new Set();
    nodes.forEach((n) => {
      if (n.protocol) set.add(n.protocol);
    });
    return Array.from(set).sort();
  }, [nodes]);

  const filteredNodes = useMemo(() => {
    const kw = filterKeyword.trim().toLowerCase();
    return nodes.filter((n) => {
      if (kw) {
        const hay = `${n.name || ''} ${n.protocol || ''} ${n.link || ''} ${n.linkAddress || ''}`.toLowerCase();
        if (!hay.includes(kw)) return false;
      }
      if (filterProtocol !== 'all' && (n.protocol || '') !== filterProtocol) return false;
      if (filterStatus === 'valid' && !n.isValid) return false;
      if (filterStatus === 'invalid' && n.isValid) return false;
      if (filterStatus === 'untested') {
        const ds = n.delayStatus || 'untested';
        if (ds !== 'untested' && ds !== '') return false;
      }
      if (filterPromoted === 'promoted' && !n.promoted) return false;
      if (filterPromoted === 'unpromoted' && n.promoted) return false;
      return true;
    });
  }, [nodes, filterKeyword, filterStatus, filterPromoted, filterProtocol]);

  const pagedNodes = useMemo(() => {
    const start = page * rowsPerPage;
    return filteredNodes.slice(start, start + rowsPerPage);
  }, [filteredNodes, page, rowsPerPage]);

  const invalidCount = useMemo(() => nodes.filter((n) => !n.isValid).length, [nodes]);
  const pageSelectedCount = useMemo(
    () => pagedNodes.filter((n) => selectedNodeIds.includes(n.id)).length,
    [pagedNodes, selectedNodeIds]
  );
  const allPageSelected = pagedNodes.length > 0 && pageSelectedCount === pagedNodes.length;
  const somePageSelected = pageSelectedCount > 0 && pageSelectedCount < pagedNodes.length;

  useEffect(() => {
    setPage(0);
  }, [filterKeyword, filterStatus, filterPromoted, filterProtocol]);

  const loadConfigs = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const res = await listGitHubCrawlConfigs();
      const list = res?.data || res || [];
      setConfigs(Array.isArray(list) ? list : []);
      if (!selectedId && Array.isArray(list) && list.length > 0) {
        setSelectedId(list[0].id);
      }
    } catch (e) {
      setError(e.message || 'load failed');
    } finally {
      setLoading(false);
    }
  }, [selectedId]);

  const loadLogs = useCallback(async (configId, afterId = 0) => {
    if (!configId) return;
    try {
      // 全量拉最新 500；增量轮询仍用较小 limit
      const res = await listGitHubCrawlLogs(configId, {
        afterId,
        limit: afterId > 0 ? 100 : GITHUB_CRAWL_LOG_MAX
      });
      const list = Array.isArray(res?.data) ? res.data : Array.isArray(res) ? res : [];
      if (afterId > 0) {
        // 前端最多缓存 500 行，与后端保留上限一致，避免长时抓取撑爆内存
        setLogs((prev) => {
          const merged = [...prev, ...list];
          return merged.length > GITHUB_CRAWL_LOG_MAX
            ? merged.slice(merged.length - GITHUB_CRAWL_LOG_MAX)
            : merged;
        });
      } else {
        setLogs(list.length > GITHUB_CRAWL_LOG_MAX ? list.slice(list.length - GITHUB_CRAWL_LOG_MAX) : list);
      }
      if (list.length > 0) {
        setLogAfterId(list[list.length - 1].id);
      }
    } catch {
      // ignore poll errors
    }
  }, []);

  const loadNodes = useCallback(async (configId) => {
    if (!configId) return;
    try {
      // 不限制条数，返回全部节点
      const res = await listGitHubCrawlNodes(configId, {});
      const list = Array.isArray(res?.data) ? res.data : Array.isArray(res) ? res : [];
      setNodes(list);
      setSelectedNodeIds((prev) => prev.filter((id) => list.some((n) => n.id === id)));
    } catch (e) {
      setError(e.message || 'load nodes failed');
    }
  }, []);

  const loadBlacklist = useCallback(async (configId) => {
    if (!configId) {
      setBlacklist([]);
      return;
    }
    try {
      const res = await listGitHubCrawlBlacklist(configId);
      const list = Array.isArray(res?.data) ? res.data : Array.isArray(res) ? res : [];
      setBlacklist(list);
    } catch (e) {
      setError(e.message || 'load blacklist failed');
    }
  }, []);

  useEffect(() => {
    loadConfigs();
  }, [loadConfigs]);

  useEffect(() => {
    if (!selected) {
      setForm(emptyForm);
      setLogs([]);
      setNodes([]);
      setBlacklist([]);
      return;
    }
    const cronExpr = selected.cronExpr || '0 */6 * * *';
    const interval = cronToInterval(cronExpr);
    setForm({
      name: selected.name || '',
      githubToken: selected.githubToken || '',
      searchKeywords: selected.searchKeywords || '',
      searchInterval: selected.searchInterval ?? 3600,
      collectionInterval: selected.collectionInterval ?? 3600,
      maxCrawlLinks: selected.maxCrawlLinks ?? 40,
      useProxy: !!selected.useProxy,
      cronExpr,
      enabled: !!selected.enabled,
      group: selected.group || 'github',
      remark: selected.remark || '',
      autoPromote: !!selected.autoPromote,
      hour: interval.hour,
      minute: interval.minute,
      testEnabled: !!selected.testEnabled,
      testCronExpr: selected.testCronExpr || '0 0 */6 * * *',
      testProfileId: selected.testProfileId ?? 0,
      testFailureThreshold: selected.testFailureThreshold ?? 3,
      testAutoDeleteEnabled: !!selected.testAutoDeleteEnabled
    });
    setLogAfterId(0);
    setPage(0);
    setSelectedNodeIds([]);
    setFilterKeyword('');
    setFilterStatus('all');
    setFilterPromoted('all');
    setFilterProtocol('all');
    loadLogs(selected.id, 0);
    loadNodes(selected.id);
    loadBlacklist(selected.id);
    setRunning(String(selected.lastStatus || '').toLowerCase() === 'running');
  }, [selected, loadLogs, loadNodes, loadBlacklist]);

  // 轮询日志（低频）；抓取中才偶尔刷新节点，避免频繁大列表重渲染
  useEffect(() => {
    if (!selectedId) return undefined;
    let tick = 0;
    const timer = setInterval(() => {
      tick += 1;
      loadLogs(selectedId, logAfterId);
      // Every ~15s while running (3 * 5s), refresh nodes once.
      if (running && tick % 3 === 0) {
        loadNodes(selectedId);
        loadConfigs();
      }
    }, 5000);
    return () => clearInterval(timer);
  }, [selectedId, logAfterId, running, loadLogs, loadNodes]);

  const handleSave = async () => {
    setSaving(true);
    setError('');
    setMessage('');
    try {
      const hour = Math.max(0, Math.min(23, Number(form.hour) || 0));
      const minute = Math.max(0, Math.min(59, Number(form.minute) || 0));
      if (hour === 0 && minute === 0) {
        setError(t('githubCrawl.errors.intervalRequired', '请设置间隔小时或分钟（不能都为 0）'));
        return;
      }
      if (form.testEnabled) {
        if (!form.testCronExpr || !String(form.testCronExpr).trim()) {
          setError(t('githubCrawl.errors.testCronRequired', '启用定时全测时必须填写 Cron 表达式'));
          return;
        }
        if (!form.testProfileId) {
          setError(t('githubCrawl.errors.testProfileRequired', '启用定时全测时必须选择节点检测策略'));
          return;
        }
        if (form.testAutoDeleteEnabled && (!Number(form.testFailureThreshold) || Number(form.testFailureThreshold) <= 0)) {
          setError(t('githubCrawl.errors.testThresholdInvalid', '启用连续失败自动删除时阈值必须大于 0'));
          return;
        }
      }
      const cronExpr = intervalToCron(hour, minute);
      const { hour: _ignoredHour, minute: _ignoredMinute, ...rest } = form;
      void _ignoredHour;
      void _ignoredMinute;
      const payload = {
        ...rest,
        cronExpr,
        hour,
        minute,
        testFailureThreshold: Number(form.testFailureThreshold) || 0
      };
      if (selectedId) {
        await updateGitHubCrawlConfig(selectedId, payload);
        setMessage(t('githubCrawl.messages.updated', '配置已更新'));
      } else {
        await createGitHubCrawlConfig(payload);
        setMessage(t('githubCrawl.messages.created', '配置已创建'));
      }
      setForm((f) => ({ ...f, hour, minute, cronExpr }));
      await loadConfigs();
    } catch (e) {
      setError(e.message || 'save failed');
    } finally {
      setSaving(false);
    }
  };


  const handleDelete = async () => {
    if (!selectedId) return;
    if (!window.confirm(t('githubCrawl.confirmDelete', '确认删除该 GitHub 抓取配置？'))) return;
    try {
      await deleteGitHubCrawlConfig(selectedId);
      setSelectedId(null);
      await loadConfigs();
      setMessage(t('githubCrawl.messages.deleted', '已删除'));
    } catch (e) {
      setError(e.message || 'delete failed');
    }
  };

  const handleToggle = async (enabled) => {
    if (!selectedId) {
      setForm((f) => ({ ...f, enabled }));
      return;
    }
    try {
      await toggleGitHubCrawlConfig(selectedId, enabled);
      setForm((f) => ({ ...f, enabled }));
      await loadConfigs();
    } catch (e) {
      setError(e.message || 'toggle failed');
    }
  };

  const handleStartStop = async () => {
    if (!selectedId) return;
    setError('');
    if (running) {
      try {
        await stopGitHubCrawl(selectedId);
        setRunning(false);
        setMessage(t('githubCrawl.messages.stopped', '抓取任务已停止'));
        await loadConfigs();
      } catch (e) {
        setError(e.message || 'stop failed');
      }
      return;
    }
    try {
      await runGitHubCrawlNow(selectedId);
      setRunning(true);
      setMessage(t('githubCrawl.messages.running', '抓取任务已启动…'));
      setTimeout(() => {
        loadLogs(selectedId, 0);
        loadNodes(selectedId);
        loadConfigs();
      }, 1500);
    } catch (e) {
      setError(e.message || 'run failed');
      setRunning(false);
      setMessage('');
    }
  };

  const handleRun = handleStartStop;

  const handleClearNodes = async () => {
    if (!selectedId) return;
    if (!window.confirm(t('githubCrawl.confirmClearNodes', '确认清空独立节点列表？'))) return;
    try {
      await clearGitHubCrawlNodes(selectedId);
      setPage(0);
      await loadNodes(selectedId);
      setMessage(t('githubCrawl.messages.cleared', '节点列表已清空'));
    } catch (e) {
      setError(e.message || 'clear failed');
    }
  };

  const handleDeleteInvalid = async () => {
    if (!selectedId) return;
    if (
      !window.confirm(
        t(
          'githubCrawl.confirmDeleteInvalid',
          '确认删除全部无效节点？已加入总节点列表的对应节点也会一并删除。'
        )
      )
    )
      return;
    try {
      const res = await deleteInvalidGitHubCrawlNodes(selectedId);
      const deleted = res?.data?.deleted ?? res?.deleted ?? 0;
      const totalRemoved = res?.data?.totalRemoved ?? res?.totalRemoved ?? 0;
      setPage(0);
      setSelectedNodeIds([]);
      await loadNodes(selectedId);
      setMessage(
        t('githubCrawl.messages.deletedInvalid', '已删除 {{n}} 个无效节点（同步清理总列表 {{m}} 个）', {
          n: deleted,
          m: totalRemoved
        })
      );
    } catch (e) {
      setError(e.message || 'delete invalid failed');
    }
  };

  const handleDeleteSelected = async () => {
    if (!selectedId || selectedNodeIds.length === 0) return;
    if (
      !window.confirm(
        t('githubCrawl.confirmDeleteSelected', '确认删除选中的 {{n}} 个节点？', { n: selectedNodeIds.length })
      )
    ) {
      return;
    }
    try {
      const res = await deleteGitHubCrawlNodes(selectedId, selectedNodeIds);
      const deleted = res?.data?.deleted ?? res?.deleted ?? selectedNodeIds.length;
      setSelectedNodeIds([]);
      await loadNodes(selectedId);
      setMessage(t('githubCrawl.messages.deletedSelected', '已删除 {{n}} 个节点', { n: deleted }));
    } catch (e) {
      setError(e.message || 'delete selected failed');
    }
  };

  const handleDeleteOne = async (nodeId) => {
    if (!selectedId || !nodeId) return;
    if (!window.confirm(t('githubCrawl.confirmDeleteOne', '确认删除该节点？'))) return;
    try {
      await deleteGitHubCrawlNodes(selectedId, [nodeId]);
      setSelectedNodeIds((prev) => prev.filter((id) => id !== nodeId));
      await loadNodes(selectedId);
    } catch (e) {
      setError(e.message || 'delete failed');
    }
  };

  const toggleSelectOne = (nodeId) => {
    setSelectedNodeIds((prev) => (prev.includes(nodeId) ? prev.filter((id) => id !== nodeId) : [...prev, nodeId]));
  };

  const toggleSelectPage = () => {
    if (allPageSelected) {
      const pageIds = new Set(pagedNodes.map((n) => n.id));
      setSelectedNodeIds((prev) => prev.filter((id) => !pageIds.has(id)));
    } else {
      setSelectedNodeIds((prev) => {
        const set = new Set(prev);
        pagedNodes.forEach((n) => set.add(n.id));
        return Array.from(set);
      });
    }
  };

  const selectAllFiltered = () => {
    setSelectedNodeIds(filteredNodes.map((n) => n.id));
  };

  const clearSelection = () => setSelectedNodeIds([]);


  const openFullTestDialog = async (nodeIds = null) => {
    // nodeIds null/undefined => all; array => selected subset (may be empty to block)
    const targets = Array.isArray(nodeIds) ? nodeIds : [];
    const isSubset = Array.isArray(nodeIds);
    if (!selectedId) return;
    if (isSubset && targets.length === 0) {
      setError(t('githubCrawl.errors.noSelectedNodes', '请先勾选节点'));
      return;
    }
    if (!isSubset && nodes.length === 0) return;
    setTestTargetIds(isSubset ? targets : []);
    setError('');
    setProfilesLoading(true);
    setTestDialogOpen(true);
    try {
      const res = await getSpeedTestConfig();
      const list = res?.data || res || [];
      const arr = Array.isArray(list) ? list : [];
      setProfiles(arr);
      const saved = Number(localStorage.getItem('github_crawl_test_profile_id') || 0);
      const initial = arr.find((p) => p.id === saved)?.id || arr[0]?.id || '';
      setSelectedProfileId(initial === '' ? '' : initial);
    } catch (e) {
      setError(e.message || 'load profiles failed');
      setProfiles([]);
    } finally {
      setProfilesLoading(false);
    }
  };

  // 加载定时全测策略下拉（懒加载：仅在展开/启用时拉一次）
  const loadTestProfiles = useCallback(async () => {
    if (profiles.length > 0 || testProfilesLoading) return;
    setTestProfilesLoading(true);
    try {
      const res = await getSpeedTestConfig();
      const list = res?.data || res || [];
      const arr = Array.isArray(list) ? list : [];
      setProfiles(arr);
    } catch {
      // ignore
    } finally {
      setTestProfilesLoading(false);
    }
  }, [profiles.length, testProfilesLoading]);

  const handleConfirmFullTest = async () => {
    if (!selectedId || !selectedProfileId) {
      setError(t('githubCrawl.messages.selectProfile', '请选择节点检测策略'));
      return;
    }
    setTesting(true);
    setError('');
    setTestDialogOpen(false);
    try {
      localStorage.setItem('github_crawl_test_profile_id', String(selectedProfileId));
      const profileName = profiles.find((p) => p.id === selectedProfileId)?.name || selectedProfileId;
      const res = await testGitHubCrawlNodes(selectedId, testTargetIds, Number(selectedProfileId));
      const asyncMode = Boolean(res?.data?.async);
      const total = res?.data?.total ?? (testTargetIds.length > 0 ? testTargetIds.length : nodes.length);
      if (asyncMode) {
        const taskId = res?.data?.taskId || res?.data?.task_id || '';
        setMessage(
          t(
            'githubCrawl.messages.fullTestStarted',
            '全测任务已启动（{{n}} 个节点，策略：{{p}}），可在右下角任务中心查看进度{{task}}',
            {
              n: total,
              p: profileName,
              task: taskId ? `（#${String(taskId).slice(-8)}）` : ''
            }
          )
        );
        // 轻量刷新节点列表；详细进度走系统任务中心
        await loadNodes(selectedId);
        for (let i = 0; i < 8; i += 1) {
          await new Promise((r) => setTimeout(r, 4000));
          await loadNodes(selectedId);
        }
        setMessage(t('githubCrawl.messages.fullTestAsyncDone', '全测任务已执行，列表已刷新（策略：{{p}}）', { p: profileName }));
      } else {
        const success = res?.data?.success ?? 0;
        const failed = res?.data?.failed ?? 0;
        await loadNodes(selectedId);
        setMessage(
          t('githubCrawl.messages.fullTestDone', '全测完成：成功 {{s}}，失败 {{f}}（策略：{{p}}）', {
            s: success,
            f: failed,
            p: profileName
          })
        );
      }
    } catch (e) {
      setError(e.message || 'full test failed');
    } finally {
      setTesting(false);
    }
  };

  const handleClearLogs = async () => {
    if (!selectedId) return;
    if (!window.confirm(t('githubCrawl.confirmClearLogs', '确认清空抓取日志？'))) return;
    try {
      await clearGitHubCrawlLogs(selectedId);
      setLogs([]);
      setLogAfterId(0);
      setMessage(t('githubCrawl.messages.logsCleared', '日志已清空'));
    } catch (e) {
      setError(e.message || 'clear logs failed');
    }
  };

  const handlePromote = async (ids) => {
    if (!selectedId) return;
    if (!ids?.length) {
      setError(t('githubCrawl.messages.noPromoteTargets', '没有可加入的节点'));
      return;
    }
    setPromoting(true);
    setError('');
    setMessage('');
    try {
      const res = await promoteGitHubCrawlNodes(selectedId, ids);
      const data = res?.data || res || {};
      const promoted = data.promoted ?? 0;
      const skipped = data.skipped ?? 0;
      const failed = data.failed ?? 0;
      await loadNodes(selectedId);
      await loadLogs(selectedId, 0);
      setSelectedNodeIds([]);
      setMessage(
        t('githubCrawl.messages.promoteResult', '加入总节点列表：成功 {{p}}，跳过 {{s}}，失败 {{f}}', {
          p: promoted,
          s: skipped,
          f: failed
        })
      );
    } catch (e) {
      setError(e.message || 'promote failed');
    } finally {
      setPromoting(false);
    }
  };

  const handleTestDelay = async (nodeId) => {
    if (!selectedId) return;
    try {
      await testGitHubCrawlNodeDelay(selectedId, [nodeId]);
      await loadNodes(selectedId);
    } catch (e) {
      setError(e.message || 'delay test failed');
    }
  };

  const handleTestSpeed = async (nodeId) => {
    if (!selectedId) return;
    try {
      await testGitHubCrawlNodeSpeed(selectedId, [nodeId]);
      await loadNodes(selectedId);
    } catch (e) {
      setError(e.message || 'speed test failed');
    }
  };

  const unpromotedValidIds = useMemo(() => nodes.filter((n) => n.isValid && !n.promoted).map((n) => n.id), [nodes]);

  const openBlacklistDialog = (item = null) => {
    if (item) {
      setEditingBlacklistId(item.id);
      setBlacklistForm({
        scope: item.scope || 'link',
        target: item.target || '',
        repo: item.repo || '',
        reason: item.reason || ''
      });
    } else {
      setEditingBlacklistId(null);
      setBlacklistForm({ scope: 'link', target: '', repo: '', reason: '' });
    }
    setBlacklistDialogOpen(true);
  };

  const handleSaveBlacklist = async () => {
    if (!selectedId) return;
    const target = (blacklistForm.target || '').trim();
    if (!target) {
      setError(t('githubCrawl.blacklist.targetRequired', '请填写目标（链接或仓库）'));
      return;
    }
    setBlacklistSaving(true);
    setError('');
    try {
      const payload = {
        scope: blacklistForm.scope || 'link',
        target,
        repo: (blacklistForm.repo || '').trim(),
        reason: (blacklistForm.reason || '').trim()
      };
      if (editingBlacklistId) {
        await updateGitHubCrawlBlacklist(selectedId, editingBlacklistId, payload);
        setMessage(t('githubCrawl.blacklist.updated', '黑名单已更新'));
      } else {
        await addGitHubCrawlBlacklist(selectedId, payload);
        setMessage(t('githubCrawl.blacklist.created', '黑名单已添加'));
      }
      setBlacklistDialogOpen(false);
      await loadBlacklist(selectedId);
    } catch (e) {
      setError(e.message || 'save blacklist failed');
    } finally {
      setBlacklistSaving(false);
    }
  };

  const handleDeleteBlacklist = async (itemId) => {
    if (!selectedId || !itemId) return;
    if (!window.confirm(t('githubCrawl.blacklist.confirmDelete', '确认删除该黑名单项？'))) return;
    try {
      await deleteGitHubCrawlBlacklist(selectedId, itemId);
      setMessage(t('githubCrawl.blacklist.deleted', '黑名单已删除'));
      await loadBlacklist(selectedId);
    } catch (e) {
      setError(e.message || 'delete blacklist failed');
    }
  };

  const visibleBlacklist = useMemo(
    () => blacklist.filter((b) => !(b.scope === 'link' && String(b.target || '').startsWith('__zero_valid__:'))),
    [blacklist]
  );

  return (
    <MainCard title={t('githubCrawl.title', 'GitHub爬虫')}>
      <Stack spacing={2}>
        {error ? <Alert severity="error">{error}</Alert> : null}
        {message ? <Alert severity="success" onClose={() => setMessage('')}>{message}</Alert> : null}

        <Grid container spacing={2}>
          {/* ── 操作栏卡片 ── */}
          <Grid item xs={12}>
            <Card variant="outlined">
              <CardContent>
                <Stack spacing={1.5}>
                  <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1} justifyContent="space-between" alignItems="center">
                    <Stack direction="row" spacing={1.5} alignItems="center">
                      <Typography variant="h5">
                        {selectedId ? t('githubCrawl.editConfig', '编辑配置') : t('githubCrawl.newConfig', '新建配置')}
                      </Typography>
                      <FormControlLabel
                        sx={{ ml: 0 }}
                        control={<Switch checked={!!form.enabled} onChange={(e) => handleToggle(e.target.checked)} />}
                        label={t('githubCrawl.fields.enabled', '定时启用')}
                      />
                    </Stack>
                    <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
                      {selectedId ? (
                        <Button
                          color={running ? 'error' : 'primary'}
                          variant="contained"
                          startIcon={running ? <IconPlayerStop size={16} /> : <IconPlayerPlay size={16} />}
                          onClick={handleRun}
                        >
                          {running ? t('githubCrawl.stop', '停止') : t('githubCrawl.start', '开始')}
                        </Button>
                      ) : null}
                      <Button variant="outlined" startIcon={<IconRefresh size={16} />} onClick={() => loadConfigs()}>
                        {t('common.refresh', '刷新')}
                      </Button>
                      <Button variant="contained" disabled={saving} onClick={handleSave}>
                        {saving ? t('common.saving', '保存中…') : t('common.save', '保存')}
                      </Button>
                      {selectedId ? (
                        <Button color="error" variant="outlined" startIcon={<IconTrash size={16} />} onClick={handleDelete}>
                          {t('common.delete', '删除')}
                        </Button>
                      ) : null}
                    </Box>
                  </Stack>
                  <Stack direction="row" spacing={1.5} alignItems="center" flexWrap="wrap" useFlexGap>
                    <Button color="warning" variant="outlined" startIcon={<IconPlus size={16} />} disabled={!selectedId} onClick={() => openBlacklistDialog()}>
                      {t('githubCrawl.blacklist.title', '黑名单')}
                    </Button>
                    <Box sx={{ flexGrow: 1 }} />
                    <Chip
                      size="small"
                      color={running ? 'error' : 'default'}
                      label={
                        running
                          ? t('githubCrawl.running', '抓取中')
                          : t('githubCrawl.idle', '未运行')
                      }
                    />
                  </Stack>
                </Stack>
              </CardContent>
            </Card>
          </Grid>

          {/* ── 基础信息卡片 ── */}
          <Grid item xs={12}>
            <Card variant="outlined">
              <CardContent>
                <Stack spacing={1.5}>
                  <Typography variant="subtitle2" color="text.primary" sx={{ fontWeight: 600 }}>
                    {t('githubCrawl.sections.basic', '基础信息')}
                  </Typography>
                  <Divider />
                  <Grid container spacing={2}>
                    <Grid item xs={12} sm={6}>
                      <TextField
                        fullWidth
                        size="small"
                        label={t('githubCrawl.fields.name', '名称')}
                        value={form.name}
                        onChange={(e) => setForm({ ...form, name: e.target.value })}
                      />
                    </Grid>
                    <Grid item xs={12} sm={6}>
                      <TextField
                        fullWidth
                        size="small"
                        label={t('githubCrawl.fields.group', '导入分组')}
                        value={form.group}
                        onChange={(e) => setForm({ ...form, group: e.target.value })}
                        helperText={t('githubCrawl.helpers.group', '加入总节点列表时的默认分组')}
                      />
                    </Grid>
                    <Grid item xs={12}>
                      <TextField
                        fullWidth
                        size="small"
                        type="password"
                        label={t('githubCrawl.fields.githubToken', 'GitHub Token')}
                        value={form.githubToken}
                        onChange={(e) => setForm({ ...form, githubToken: e.target.value })}
                        helperText={t(
                          'githubCrawl.helpers.githubToken',
                          '必填。Code Search 需要 PAT（classic 勾选 code 读权限）'
                        )}
                        autoComplete="off"
                      />
                    </Grid>
                  </Grid>
                </Stack>
              </CardContent>
            </Card>
          </Grid>

          {/* ── 抓取参数卡片 ── */}
          <Grid item xs={12}>
            <Card variant="outlined">
              <CardContent>
                <Stack spacing={1.5}>
                  <Typography variant="subtitle2" color="text.primary" sx={{ fontWeight: 600 }}>
                    {t('githubCrawl.sections.crawl', '抓取参数')}
                  </Typography>
                  <Divider />
                  <Grid container spacing={2}>
                    <Grid item xs={12}>
                      <TextField
                        fullWidth
                        size="small"
                        multiline
                        minRows={2}
                        label={t('githubCrawl.fields.searchKeywords', '搜索关键字')}
                        value={form.searchKeywords}
                        onChange={(e) => setForm({ ...form, searchKeywords: e.target.value })}
                        helperText={t('githubCrawl.helpers.searchKeywords', '多行或逗号分隔，推荐使用 clash free nodes yaml / mihomo free subscription 等')}
                      />
                    </Grid>
                    <Grid item xs={12} sm={6}>
                      <TextField
                        fullWidth
                        size="small"
                        type="number"
                        label={t('githubCrawl.fields.searchInterval', '搜索间隔(秒)')}
                        value={form.searchInterval}
                        onChange={(e) => setForm({ ...form, searchInterval: Number(e.target.value) || 0 })}
                      />
                    </Grid>
                    <Grid item xs={12} sm={6}>
                      <TextField
                        fullWidth
                        size="small"
                        type="number"
                        label={t('githubCrawl.fields.collectionInterval', '采集间隔(秒)')}
                        value={form.collectionInterval}
                        onChange={(e) => setForm({ ...form, collectionInterval: Number(e.target.value) || 0 })}
                      />
                    </Grid>
                    <Grid item xs={12}>
                      <TextField
                        fullWidth
                        size="small"
                        type="number"
                        label={t('githubCrawl.fields.maxCrawlLinks', '最多爬取链接数')}
                        value={form.maxCrawlLinks}
                        onChange={(e) => setForm({ ...form, maxCrawlLinks: Number(e.target.value) || 0 })}
                        helperText={t('githubCrawl.helpers.maxCrawlLinks', '目标有效入库节点数（测速通过后计数），按仓库更新时间从新到旧抓取，同库最多 3 个文件，达到目标即停止')}
                        inputProps={{ min: 1, max: 200 }}
                      />
                    </Grid>
                    <Grid item xs={12} sm={6}>
                      <FormControlLabel
                        control={
                          <Switch
                            checked={!!form.autoPromote}
                            onChange={(e) => setForm({ ...form, autoPromote: e.target.checked })}
                          />
                        }
                        label={t('githubCrawl.fields.autoPromote', '抓取完成后自动加入总节点列表')}
                      />
                    </Grid>
                    <Grid item xs={12} sm={6}>
                      <FormControlLabel
                        control={
                          <Switch
                            checked={!!form.useProxy}
                            onChange={(e) => setForm({ ...form, useProxy: e.target.checked })}
                          />
                        }
                        label={t('githubCrawl.fields.useProxy', '拉取时尝试使用代理')}
                      />
                      <Typography variant="caption" color="text.secondary" display="block">
                        {t(
                          'githubCrawl.helpers.useProxy',
                          '开启后搜索/拉取文件时优先走可用代理节点；无可用代理则直连'
                        )}
                      </Typography>
                    </Grid>
                  </Grid>
                </Stack>
              </CardContent>
            </Card>
          </Grid>

          {/* ── 定时调度卡片 ── */}
          <Grid item xs={12}>
            <Card variant="outlined">
              <CardContent>
                <Stack spacing={1.5}>
                  <Typography variant="subtitle2" color="text.primary" sx={{ fontWeight: 600 }}>
                    {t('githubCrawl.sections.schedule', '定时调度')}
                  </Typography>
                  <Divider />
                  <Grid container spacing={2}>
                    <Grid item xs={12} sm={6}>
                      <Stack direction="row" spacing={1.5}>
                        <TextField
                          fullWidth
                          size="small"
                          type="number"
                          label={t('githubCrawl.fields.intervalHour', '间隔小时')}
                          value={form.hour}
                          onChange={(e) => setForm({ ...form, hour: Number(e.target.value) || 0 })}
                          inputProps={{ min: 0, max: 23 }}
                        />
                        <TextField
                          fullWidth
                          size="small"
                          type="number"
                          label={t('githubCrawl.fields.intervalMinute', '间隔分钟')}
                          value={form.minute}
                          onChange={(e) => setForm({ ...form, minute: Number(e.target.value) || 0 })}
                          inputProps={{ min: 0, max: 59 }}
                        />
                      </Stack>
                    </Grid>
                    <Grid item xs={12} sm={6}>
                      <Box sx={{ display: 'flex', alignItems: 'center', height: '100%' }}>
                        <Stack spacing={0.5} sx={{ width: '100%' }}>
                          <TextField
                            fullWidth
                            size="small"
                            label={t('githubCrawl.fields.cronExpr', 'Cron 表达式')}
                            value={form.cronExpr}
                            onChange={(e) => setForm({ ...form, cronExpr: e.target.value })}
                            placeholder="0 */6 * * *"
                            helperText={t(
                              'githubCrawl.helpers.interval',
                              '按间隔调度，保存时自动转为 Cron（例：6 小时 → 0 */6 * * *）'
                            )}
                          />
                          {form.hour || form.minute ? (
                            <Typography variant="caption" color="text.secondary">
                              {t('githubCrawl.helpers.generatedCron', '生成 Cron：{{cron}}', {
                                cron: intervalToCron(form.hour, form.minute)
                              })}
                            </Typography>
                          ) : null}
                        </Stack>
                      </Box>
                    </Grid>
                  </Grid>
                </Stack>
              </CardContent>
            </Card>
          </Grid>

          {/* ── 独立节点定时全测卡片 ── */}
          <Grid item xs={12}>
            <Card variant="outlined">
              <CardContent>
                <Stack spacing={1.5}>
                  <Stack direction="row" alignItems="center" justifyContent="space-between" flexWrap="wrap" useFlexGap>
                    <Typography variant="subtitle2" color="text.primary" sx={{ fontWeight: 600 }}>
                      {t('githubCrawl.testSchedule.title', '独立节点定时全测')}
                    </Typography>
                    {selected?.lastTestTime ? (
                      <Typography variant="caption" color="text.secondary">
                        {t('githubCrawl.testSchedule.lastRun', '上次：{{time}} · {{status}}', {
                          time: new Date(selected.lastTestTime).toLocaleString(),
                          status: selected.lastTestStatus || '-'
                        })}
                      </Typography>
                    ) : null}
                  </Stack>
                  <Divider />
                  <Grid container spacing={2}>
                    <Grid item xs={12} sm={4}>
                      <FormControlLabel
                        control={
                          <Switch
                            checked={!!form.testEnabled}
                            onChange={(e) => {
                              const on = e.target.checked;
                              setForm({ ...form, testEnabled: on });
                              if (on) loadTestProfiles();
                            }}
                          />
                        }
                        label={t('githubCrawl.testSchedule.enable', '启用定时全测')}
                      />
                    </Grid>
                    <Grid item xs={12} sm={4}>
                      <TextField
                        fullWidth
                        size="small"
                        label={t('githubCrawl.testSchedule.cron', '全测 Cron')}
                        value={form.testCronExpr}
                        onChange={(e) => setForm({ ...form, testCronExpr: e.target.value })}
                        placeholder="0 0 */6 * * *"
                        helperText={t(
                          'githubCrawl.testSchedule.cronHelp',
                          '标准 5 字段 cron；空表示不调度（仍可手动触发）'
                        )}
                        disabled={!form.testEnabled}
                      />
                    </Grid>
                    <Grid item xs={12} sm={4}>
                      <FormControl fullWidth size="small" disabled={!form.testEnabled}>
                        <InputLabel id="gh-test-profile-label">
                          {t('githubCrawl.testSchedule.profile', '检测策略')}
                        </InputLabel>
                        <Select
                          labelId="gh-test-profile-label"
                          label={t('githubCrawl.testSchedule.profile', '检测策略')}
                          value={form.testProfileId || ''}
                          onChange={(e) => setForm({ ...form, testProfileId: Number(e.target.value) || 0 })}
                          onOpen={() => loadTestProfiles()}
                        >
                          {profiles.length === 0 ? (
                            <MenuItem value="" disabled>
                              {testProfilesLoading
                                ? t('common.loading', '加载中…')
                                : t('githubCrawl.noProfiles', '暂无节点检测策略，请先到「节点检测」中创建策略。')}
                            </MenuItem>
                          ) : null}
                          {profiles.map((p) => (
                            <MenuItem key={p.id} value={p.id}>
                              {p.name}
                              {p.mode ? ` (${p.mode})` : ''}
                            </MenuItem>
                          ))}
                        </Select>
                      </FormControl>
                    </Grid>
                    <Grid item xs={12} sm={6}>
                      <Stack direction="row" spacing={2} alignItems="center" flexWrap="wrap" useFlexGap>
                        <FormControlLabel
                          control={
                            <Switch
                              checked={!!form.testAutoDeleteEnabled}
                              onChange={(e) => setForm({ ...form, testAutoDeleteEnabled: e.target.checked })}
                              disabled={!form.testEnabled}
                            />
                          }
                          label={t('githubCrawl.testSchedule.autoDelete', '连续失败自动删除')}
                        />
                        <TextField
                          size="small"
                          type="number"
                          label={t('githubCrawl.testSchedule.threshold', '失败次数阈值')}
                          value={form.testFailureThreshold}
                          onChange={(e) => setForm({ ...form, testFailureThreshold: Number(e.target.value) || 0 })}
                          inputProps={{ min: 1, max: 100 }}
                          sx={{ width: 140 }}
                          disabled={!form.testEnabled || !form.testAutoDeleteEnabled}
                        />
                      </Stack>
                      <Typography variant="caption" color="text.secondary" display="block">
                        {t(
                          'githubCrawl.testSchedule.autoDeleteHelp',
                          '开启后：节点连续失败达到阈值时自动从独立节点列表删除；若曾加入总表会一并清理。建议阈值 ≥ 3。'
                        )}
                      </Typography>
                    </Grid>
                  </Grid>
                </Stack>
              </CardContent>
            </Card>
          </Grid>

          {/* ── 备注卡片 ── */}
          <Grid item xs={12}>
            <Card variant="outlined">
              <CardContent>
                <Stack spacing={1.5}>
                  <Typography variant="subtitle2" color="text.primary" sx={{ fontWeight: 600 }}>
                    {t('githubCrawl.sections.remark', '备注')}
                  </Typography>
                  <Divider />
                  <Grid container spacing={2}>
                    <Grid item xs={12}>
                      <TextField
                        fullWidth
                        size="small"
                        label={t('githubCrawl.fields.remark', '备注')}
                        value={form.remark}
                        onChange={(e) => setForm({ ...form, remark: e.target.value })}
                      />
                    </Grid>
                  </Grid>
                </Stack>
              </CardContent>
            </Card>
          </Grid>
        </Grid>

        {/* 独立节点列表：整行 + 筛选 + 勾选管理 */}
        <Box sx={{ width: '100%' }}>
          <Card variant="outlined" sx={{ width: '100%' }}>
            <CardContent>
              <Stack direction="row" justifyContent="space-between" alignItems="center" mb={1} flexWrap="wrap" gap={1}>
                <Typography variant="subtitle1">
                  {t('githubCrawl.validNodes', '独立节点列表')} ({filteredNodes.length}
                  {filteredNodes.length !== nodes.length ? ` / ${nodes.length}` : ''})
                  {selectedNodeIds.length > 0 ? (
                    <Chip
                      size="small"
                      color="primary"
                      label={t('githubCrawl.selectedCount', '已选 {{n}}', { n: selectedNodeIds.length })}
                      sx={{ ml: 1 }}
                      onDelete={clearSelection}
                    />
                  ) : null}
                </Typography>
                <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
                  <Button size="small" startIcon={<IconRefresh size={14} />} onClick={() => selectedId && loadNodes(selectedId)}>
                    {t('common.refresh', '刷新')}
                  </Button>
                  <Button
                    size="small"
                    variant="outlined"
                    startIcon={testing ? <CircularProgress size={14} /> : <IconGauge size={14} />}
                    disabled={!selectedId || nodes.length === 0 || testing}
                    onClick={() => openFullTestDialog(null)}
                  >
                    {t('githubCrawl.testAll', '全测')}
                  </Button>
                  <Button
                    size="small"
                    variant="outlined"
                    color="secondary"
                    startIcon={<IconGauge size={14} />}
                    disabled={!selectedId || selectedNodeIds.length === 0 || testing}
                    onClick={() => openFullTestDialog(selectedNodeIds)}
                  >
                    {t('githubCrawl.testSelected', '测选中')} ({selectedNodeIds.length})
                  </Button>
                  <Button
                    size="small"
                    variant="contained"
                    startIcon={<IconDownload size={14} />}
                    disabled={promoting || (selectedNodeIds.length === 0 && unpromotedValidIds.length === 0)}
                    onClick={() =>
                      handlePromote(selectedNodeIds.length > 0 ? selectedNodeIds : unpromotedValidIds)
                    }
                  >
                    {selectedNodeIds.length > 0
                      ? (promoting ? t('githubCrawl.promoting', '加入中…') : t('githubCrawl.promoteSelected', '加入总列表') + ` (${selectedNodeIds.length})`)
                      : t('githubCrawl.promoteAll', '全部加入总列表')}
                  </Button>
                  <Button
                    size="small"
                    color="error"
                    variant="outlined"
                    startIcon={<IconTrash size={14} />}
                    disabled={!selectedId || selectedNodeIds.length === 0}
                    onClick={handleDeleteSelected}
                  >
                    {t('githubCrawl.deleteSelected', '删除选中')} ({selectedNodeIds.length})
                  </Button>
                  <Button
                    size="small"
                    color="warning"
                    startIcon={<IconTrash size={14} />}
                    disabled={!selectedId || invalidCount === 0}
                    onClick={handleDeleteInvalid}
                  >
                    {t('githubCrawl.deleteInvalid', '删除无效')} ({invalidCount})
                  </Button>
                  <Button size="small" color="error" startIcon={<IconTrash size={14} />} onClick={handleClearNodes}>
                    {t('githubCrawl.clearNodes', '清空')}
                  </Button>
                </Stack>
              </Stack>

              <Stack direction={{ xs: 'column', md: 'row' }} spacing={1} mb={1.5} flexWrap="wrap" useFlexGap>
                <TextField
                  size="small"
                  label={t('githubCrawl.filter.keyword', '关键词')}
                  placeholder={t('githubCrawl.filter.keywordPh', '名称 / 协议 / 链接')}
                  value={filterKeyword}
                  onChange={(e) => setFilterKeyword(e.target.value)}
                  sx={{ minWidth: 220, flex: 1 }}
                />
                <FormControl size="small" sx={{ minWidth: 140 }}>
                  <InputLabel id="gh-filter-status">{t('githubCrawl.filter.status', '状态')}</InputLabel>
                  <Select
                    labelId="gh-filter-status"
                    label={t('githubCrawl.filter.status', '状态')}
                    value={filterStatus}
                    onChange={(e) => setFilterStatus(e.target.value)}
                  >
                    <MenuItem value="all">{t('common.all', '全部')}</MenuItem>
                    <MenuItem value="valid">{t('githubCrawl.filter.valid', '有效')}</MenuItem>
                    <MenuItem value="invalid">{t('githubCrawl.filter.invalid', '无效')}</MenuItem>
                    <MenuItem value="untested">{t('githubCrawl.filter.untested', '未测')}</MenuItem>
                  </Select>
                </FormControl>
                <FormControl size="small" sx={{ minWidth: 140 }}>
                  <InputLabel id="gh-filter-promoted">{t('githubCrawl.filter.promoted', '导入状态')}</InputLabel>
                  <Select
                    labelId="gh-filter-promoted"
                    label={t('githubCrawl.filter.promoted', '导入状态')}
                    value={filterPromoted}
                    onChange={(e) => setFilterPromoted(e.target.value)}
                  >
                    <MenuItem value="all">{t('common.all', '全部')}</MenuItem>
                    <MenuItem value="promoted">{t('githubCrawl.filter.promotedYes', '已加入')}</MenuItem>
                    <MenuItem value="unpromoted">{t('githubCrawl.filter.promotedNo', '未加入')}</MenuItem>
                  </Select>
                </FormControl>
                <FormControl size="small" sx={{ minWidth: 140 }}>
                  <InputLabel id="gh-filter-protocol">{t('githubCrawl.filter.protocol', '协议')}</InputLabel>
                  <Select
                    labelId="gh-filter-protocol"
                    label={t('githubCrawl.filter.protocol', '协议')}
                    value={filterProtocol}
                    onChange={(e) => setFilterProtocol(e.target.value)}
                  >
                    <MenuItem value="all">{t('common.all', '全部')}</MenuItem>
                    {protocolOptions.map((p) => (
                      <MenuItem key={p} value={p}>
                        {p}
                      </MenuItem>
                    ))}
                  </Select>
                </FormControl>
                <Button
                  size="small"
                  onClick={() => {
                    setFilterKeyword('');
                    setFilterStatus('all');
                    setFilterPromoted('all');
                    setFilterProtocol('all');
                  }}
                >
                  {t('common.reset', '重置')}
                </Button>
                <Button size="small" disabled={filteredNodes.length === 0} onClick={selectAllFiltered}>
                  {t('githubCrawl.selectAllFiltered', '全选筛选结果')} ({filteredNodes.length})
                </Button>
              </Stack>

              <Divider sx={{ mb: 1 }} />
              <Box sx={{ maxHeight: 420, overflow: 'auto', width: '100%' }}>
                <Table size="small" stickyHeader>
                  <TableHead>
                    <TableRow>
                      <TableCell padding="checkbox">
                        <Checkbox
                          indeterminate={somePageSelected}
                          checked={allPageSelected}
                          onChange={toggleSelectPage}
                          disabled={pagedNodes.length === 0}
                        />
                      </TableCell>
                      <TableCell>{t('githubCrawl.node.name', '名称')}</TableCell>
                      <TableCell>{t('githubCrawl.node.protocol', '协议')}</TableCell>
                      <TableCell>{t('githubCrawl.node.delay', '延迟')}</TableCell>
                      <TableCell>{t('githubCrawl.node.speed', '速度')}</TableCell>
                      <TableCell>{t('githubCrawl.node.status', '状态')}</TableCell>
                      <TableCell align="right">{t('common.actions', '操作')}</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {pagedNodes.map((n) => {
                      const checked = selectedNodeIds.includes(n.id);
                      return (
                        <TableRow key={n.id} hover selected={checked}>
                          <TableCell padding="checkbox">
                            <Checkbox checked={checked} onChange={() => toggleSelectOne(n.id)} />
                          </TableCell>
                          <TableCell>
                            <Typography variant="body2" noWrap sx={{ maxWidth: 220 }} title={n.name}>
                              {n.name}
                            </Typography>
                            {n.linkAddress ? (
                              <Typography variant="caption" color="text.secondary" noWrap display="block" sx={{ maxWidth: 220 }}>
                                {n.linkAddress}
                                {n.linkPort ? `:${n.linkPort}` : ''}
                              </Typography>
                            ) : null}
                          </TableCell>
                          <TableCell>
                            <Chip size="small" label={n.protocol || '-'} />
                          </TableCell>
                          <TableCell>
                            {n.delayStatus === 'success' ? `${n.delayTime}ms` : n.delayStatus || '-'}
                          </TableCell>
                          <TableCell>
                            {n.speedStatus === 'success'
                              ? `${n.speed?.toFixed?.(2) ?? n.speed} MB/s`
                              : n.speedStatus || '-'}
                          </TableCell>
                          <TableCell>
                            <Stack direction="row" spacing={0.5} flexWrap="wrap" useFlexGap>
                              {n.isValid ? (
                                <Chip size="small" color="success" label={t('githubCrawl.filter.valid', '有效')} />
                              ) : (
                                <Chip size="small" label={t('githubCrawl.filter.invalid', '无效')} />
                              )}
                              {n.promoted ? (
                                <Chip size="small" color="info" label={t('githubCrawl.filter.promotedYes', '已加入')} />
                              ) : null}
                              {Number(n.consecutiveFailures) > 0 ? (
                                <Chip
                                  size="small"
                                  color="warning"
                                  variant="outlined"
                                  label={t('githubCrawl.consecutiveFailures', '连败 {{n}}', { n: n.consecutiveFailures })}
                                />
                              ) : null}
                            </Stack>
                          </TableCell>
                          <TableCell align="right">
                            <Tooltip title={t('githubCrawl.testDelay', '测延时')}>
                              <IconButton size="small" onClick={() => handleTestDelay(n.id)}>
                                <IconClock size={16} />
                              </IconButton>
                            </Tooltip>
                            <Tooltip title={t('githubCrawl.testSpeed', '测速')}>
                              <IconButton size="small" onClick={() => handleTestSpeed(n.id)}>
                                <IconGauge size={16} />
                              </IconButton>
                            </Tooltip>
                            {!n.promoted ? (
                              <Tooltip title={t('githubCrawl.promote', '加入总列表')}>
                                <IconButton size="small" color="primary" onClick={() => handlePromote([n.id])}>
                                  <IconDownload size={16} />
                                </IconButton>
                              </Tooltip>
                            ) : null}
                            <Tooltip title={t('common.delete', '删除')}>
                              <IconButton size="small" color="error" onClick={() => handleDeleteOne(n.id)}>
                                <IconTrash size={16} />
                              </IconButton>
                            </Tooltip>
                          </TableCell>
                        </TableRow>
                      );
                    })}
                    {pagedNodes.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={7}>
                          <Typography variant="body2" color="text.secondary" align="center">
                            {nodes.length === 0
                              ? t('githubCrawl.noNodes', '暂无节点，先执行抓取')
                              : t('githubCrawl.noFilterMatch', '无匹配节点，请调整筛选条件')}
                          </Typography>
                        </TableCell>
                      </TableRow>
                    ) : null}
                  </TableBody>
                </Table>
              </Box>
              <TablePagination
                component="div"
                count={filteredNodes.length}
                page={page}
                onPageChange={(_, newPage) => setPage(newPage)}
                rowsPerPage={rowsPerPage}
                onRowsPerPageChange={(e) => {
                  const v = parseInt(e.target.value, 10);
                  setRowsPerPage(v);
                  localStorage.setItem('github_crawl_rowsPerPage', String(v));
                  setPage(0);
                }}
                rowsPerPageOptions={[10, 20, 50, 100, 200]}
                labelRowsPerPage={t('common.rowsPerPage', '每页行数')}
                labelDisplayedRows={({ from, to, count }) =>
                  t('common.displayedRows', '{{from}}-{{to}} / {{count}}', { from, to, count })
                }
              />
            </CardContent>
          </Card>
        </Box>

        {/* 黑名单列表 */}
        <Box sx={{ width: '100%' }}>
          <Card variant="outlined" sx={{ width: '100%' }}>
            <CardContent>
              <Stack direction="row" justifyContent="space-between" alignItems="center" mb={2}>
                <Typography variant="subtitle1">
                  {t('githubCrawl.blacklist.title', '黑名单')} ({visibleBlacklist.length})
                </Typography>
                <Stack direction="row" spacing={1}>
                  <Button
                    size="small"
                    variant="outlined"
                    startIcon={<IconRefresh size={14} />}
                    onClick={() => selectedId && loadBlacklist(selectedId)}
                  >
                    {t('common.refresh', '刷新')}
                  </Button>
                  <Button
                    size="small"
                    color="primary"
                    variant="contained"
                    startIcon={<IconPlus size={14} />}
                    onClick={() => openBlacklistDialog()}
                  >
                    {t('githubCrawl.blacklist.add', '添加')}
                  </Button>
                </Stack>
              </Stack>

              <Box sx={{ maxHeight: 280, overflow: 'auto', width: '100%' }}>
                <Table size="small" stickyHeader sx={{ tableLayout: 'fixed', width: '100%' }}>
                  <TableHead>
                    <TableRow>
                      <TableCell sx={{ width: { xs: 180, sm: 280, md: 360 } }}>
                        {t('githubCrawl.blacklist.target', '目标')}
                      </TableCell>
                      <TableCell sx={{ width: 90 }}>
                        {t('githubCrawl.blacklist.scopeLink', '范围')}
                      </TableCell>
                      <TableCell sx={{ width: { xs: 120, sm: 200, md: 280 } }}>
                        {t('githubCrawl.blacklist.reason', '原因')}
                      </TableCell>
                      <TableCell align="right" sx={{ width: 100 }}>
                        {t('common.actions', '操作')}
                      </TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {visibleBlacklist.map((item) => (
                      <TableRow key={item.id} hover>
                        <TableCell sx={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                          <Box component="span" title={item.target} sx={{ display: 'block', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                            {item.target}
                          </Box>
                        </TableCell>
                        <TableCell>
                          <Chip size="small" label={t(`githubCrawl.blacklist.scope${item.scope === 'repo' ? 'Repo' : 'Link'}`, item.scope)} />
                        </TableCell>
                        <TableCell sx={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                          <Box component="span" title={item.reason || '-'} sx={{ display: 'block', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                            {item.reason || '-'}
                          </Box>
                        </TableCell>
                        <TableCell align="right">
                          <IconButton size="small" color="warning" onClick={() => openBlacklistDialog(item)}>
                            <IconEdit size={16} />
                          </IconButton>
                          <IconButton size="small" color="error" onClick={() => handleDeleteBlacklist(item.id)}>
                            <IconTrash size={16} />
                          </IconButton>
                        </TableCell>
                      </TableRow>
                    ))}
                    {visibleBlacklist.length === 0 && (
                      <TableRow>
                        <TableCell colSpan={4}>
                          <Typography variant="body2" color="text.secondary" align="center">
                            {t('githubCrawl.blacklist.noRules', '暂无黑名单项')}
                          </Typography>
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
               </Box>
             </CardContent>
           </Card>
         </Box>

        {/* 抓取日志：整行占满，倒序 */}
        <Box sx={{ width: '100%' }}>
          <Card variant="outlined" sx={{ width: '100%' }}>
            <CardContent>
              <Stack direction="row" justifyContent="space-between" alignItems="center" mb={1}>
                <Typography variant="subtitle1">{t('githubCrawl.logs', '抓取日志')}</Typography>
                <Stack direction="row" spacing={1}>
                  <Button size="small" onClick={() => selectedId && loadLogs(selectedId, 0)}>
                    {t('common.refresh', '刷新')}
                  </Button>
                  <Button
                    size="small"
                    color="error"
                    disabled={!selectedId || logs.length === 0}
                    startIcon={<IconTrash size={14} />}
                    onClick={handleClearLogs}
                  >
                    {t('githubCrawl.clearLogs', '清空')}
                  </Button>
                </Stack>
              </Stack>
              <Box
                sx={{
                  width: '100%',
                  maxWidth: '100%',
                  height: 360,
                  overflow: 'auto',
                  bgcolor: 'background.default',
                  borderRadius: 1,
                  p: 1,
                  fontFamily: 'monospace',
                  fontSize: 12,
                  boxSizing: 'border-box'
                }}
              >
                {displayLogs.length === 0 ? (
                  <Typography variant="body2" color="text.secondary">
                    {t('githubCrawl.noLogs', '暂无日志，点击「开始」启动抓取')}
                  </Typography>
                ) : (
                  displayLogs.map((log) => (
                    <Box
                      key={log.id}
                      sx={{
                        mb: 0.5,
                        width: '100%',
                        whiteSpace: 'pre-wrap',
                        wordBreak: 'break-word',
                        color:
                          log.level === 'error' ? 'error.main' : log.level === 'warn' ? 'warning.main' : 'text.primary'
                      }}
                    >
                      <Typography component="span" variant="caption" color="text.secondary" sx={{ mr: 1 }}>
                        {log.createdAt ? new Date(log.createdAt).toLocaleTimeString() : ''}
                      </Typography>
                      [{log.level}] {log.message}
                    </Box>
                  ))
                )}
              </Box>
            </CardContent>
          </Card>
        </Box>
      </Stack>

      <Dialog open={testDialogOpen} onClose={() => !testing && setTestDialogOpen(false)} fullWidth maxWidth="xs">
        <DialogTitle>{t('githubCrawl.fullTestTitle', '全测 - 选择节点检测策略')}</DialogTitle>
        <DialogContent>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {testTargetIds.length > 0
              ? t(
                  'githubCrawl.fullTestHintSelected',
                  '将使用所选策略对已勾选的 {{n}} 个节点执行全测（TCP 仅延时 / Mihomo 延时+测速）。',
                  { n: testTargetIds.length }
                )
              : t(
                  'githubCrawl.fullTestHint',
                  '将使用所选策略的测速 URL、超时、并发与模式（TCP 仅延时 / Mihomo 延时+测速）对独立节点列表执行全测。'
                )}
          </Typography>
          {profilesLoading ? (
            <Stack alignItems="center" py={2}>
              <CircularProgress size={28} />
            </Stack>
          ) : profiles.length === 0 ? (
            <Alert severity="warning">
              {t('githubCrawl.noProfiles', '暂无节点检测策略，请先到「节点检测」中创建策略。')}
            </Alert>
          ) : (
            <FormControl fullWidth size="small" sx={{ mt: 1 }}>
              <InputLabel id="github-crawl-profile-label">{t('githubCrawl.profile', '检测策略')}</InputLabel>
              <Select
                labelId="github-crawl-profile-label"
                label={t('githubCrawl.profile', '检测策略')}
                value={selectedProfileId}
                onChange={(e) => setSelectedProfileId(e.target.value)}
              >
                {profiles.map((p) => (
                  <MenuItem key={p.id} value={p.id}>
                    {p.name}
                    {p.mode ? ` (${p.mode})` : ''}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setTestDialogOpen(false)} disabled={testing}>
            {t('common.cancel', '取消')}
          </Button>
          <Button
            variant="contained"
            onClick={handleConfirmFullTest}
            disabled={testing || profilesLoading || !selectedProfileId || profiles.length === 0}
          >
            {t('githubCrawl.startFullTest', '开始全测')}
          </Button>
        </DialogActions>
      </Dialog>

      <Dialog open={blacklistDialogOpen} onClose={() => setBlacklistDialogOpen(false)} fullWidth maxWidth="sm">
        <DialogTitle>
          {editingBlacklistId
            ? t('githubCrawl.blacklist.edit', '编辑黑名单')
            : t('githubCrawl.blacklist.add', '添加黑名单')}
        </DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 1 }}>
            <FormControl fullWidth size="small">
              <InputLabel>{t('githubCrawl.blacklist.scopeLink', '范围')}</InputLabel>
              <Select
                value={blacklistForm.scope}
                onChange={(e) => setBlacklistForm({ ...blacklistForm, scope: e.target.value })}
                label={t('githubCrawl.blacklist.scopeLink', '范围')}
              >
                <MenuItem value="link">{t('githubCrawl.blacklist.scopeLink', '链接')}</MenuItem>
                <MenuItem value="repo">{t('githubCrawl.blacklist.scopeRepo', '仓库')}</MenuItem>
              </Select>
            </FormControl>
            <TextField
              fullWidth
              size="small"
              label={t('githubCrawl.blacklist.target', '目标')}
              placeholder={blacklistForm.scope === 'repo' ? 'owner/repo' : 'https://example.com/path/to/file.yaml'}
              value={blacklistForm.target}
              onChange={(e) => setBlacklistForm({ ...blacklistForm, target: e.target.value })}
            />
            <TextField
              fullWidth
              size="small"
              label={t('githubCrawl.blacklist.reason', '原因')}
              placeholder="抓取失败 / 404 / 多次0有效"
              value={blacklistForm.reason}
              onChange={(e) => setBlacklistForm({ ...blacklistForm, reason: e.target.value })}
            />
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setBlacklistDialogOpen(false)}>{t('common.cancel', '取消')}</Button>
          <Button variant="contained" onClick={handleSaveBlacklist} disabled={blacklistSaving}>
            {blacklistSaving ? t('common.saving', '保存中…') : t('common.save', '保存')}
          </Button>
        </DialogActions>
      </Dialog>
    </MainCard>
  );
}

