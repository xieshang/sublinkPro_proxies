import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { alpha, useTheme } from '@mui/material/styles';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import CircularProgress from '@mui/material/CircularProgress';
import Divider from '@mui/material/Divider';
import Stack from '@mui/material/Stack';
import Typography from '@mui/material/Typography';

import ChecklistIcon from '@mui/icons-material/Checklist';
import UpgradeIcon from '@mui/icons-material/Upgrade';

import MainCard from 'ui-component/cards/MainCard';
import ConfirmDialog from 'components/ConfirmDialog';
import { listRemoteVersions, startUpgrade } from 'api/updater';

export default function RemoteVersionsPanel({ busy, onNotify, onStarted }) {
  const theme = useTheme();
  const { t } = useTranslation();
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(false);
  const [confirmTarget, setConfirmTarget] = useState(null);
  const [starting, setStarting] = useState(false);

  const load = useCallback(async () => {
    try {
      setLoading(true);
      const res = await listRemoteVersions();
      setData(res?.data || null);
    } catch (error) {
      onNotify?.(error?.message || t('systemUpdate.remote.loadFailed'), 'error');
    } finally {
      setLoading(false);
    }
  }, [onNotify, t]);

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleUpgrade = async () => {
    if (!confirmTarget) return;
    try {
      setStarting(true);
      await startUpgrade(confirmTarget.version || '');
      onNotify?.(t('systemUpdate.messages.upgradeStarted', { version: confirmTarget.version || t('systemUpdate.remote.latest') }));
      setConfirmTarget(null);
      onStarted?.();
    } catch (error) {
      onNotify?.(error?.message || t('systemUpdate.messages.upgradeStartFailed'), 'error');
    } finally {
      setStarting(false);
    }
  };

  const versions = data?.versions || [];
  const isTemplate = data?.sourceType === 'template';

  return (
    <MainCard
      title={
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
          <Box
            sx={{
              width: 36,
              height: 36,
              borderRadius: 2,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              backgroundColor: alpha(theme.palette.primary.main, theme.palette.mode === 'dark' ? 0.18 : 0.1),
              border: `1px solid ${alpha(theme.palette.primary.main, theme.palette.mode === 'dark' ? 0.32 : 0.18)}`
            }}
          >
            <ChecklistIcon fontSize="small" sx={{ color: theme.palette.primary.main }} />
          </Box>
          {t('systemUpdate.remote.title')}
        </Box>
      }
      secondary={
        <Button size="small" variant="outlined" startIcon={loading ? <CircularProgress size={14} /> : <ChecklistIcon />} onClick={load} disabled={loading}>
          {t('systemUpdate.remote.check')}
        </Button>
      }
      sx={{ mb: 3 }}
    >
      {isTemplate ? (
        <Box>
          <Typography variant="body2" color="text.secondary">
            {t('systemUpdate.remote.templateModeTip')}
          </Typography>
          {data?.renderedUrl && (
            <Typography variant="caption" sx={{ display: 'block', mt: 1, wordBreak: 'break-all', fontFamily: '"JetBrains Mono", monospace' }}>
              {data.renderedUrl}
            </Typography>
          )}
          <Button
            sx={{ mt: 2 }}
            size="small"
            variant="contained"
            startIcon={<UpgradeIcon />}
            disabled={busy}
            onClick={() => setConfirmTarget({ version: '' })}
          >
            {t('systemUpdate.remote.upgradeLatest')}
          </Button>
        </Box>
      ) : versions.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          {t('systemUpdate.remote.empty')}
        </Typography>
      ) : (
        <Stack spacing={0} divider={<Divider flexItem />}>
          {versions.map((v) => (
            <Box key={v.version} sx={{ py: 1.5 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
                <Chip size="small" label={v.version} sx={{ fontWeight: 700 }} />
                {v.isLatest && (
                  <Chip size="small" color="primary" variant="outlined" label={t('systemUpdate.remote.latest')} />
                )}
                {v.isCurrent && <Chip size="small" color="success" variant="outlined" label={t('systemUpdate.remote.current')} />}
                {!v.installable && <Chip size="small" variant="outlined" label={t('systemUpdate.remote.notInstallable')} disabled />}
                <Box sx={{ flex: 1 }} />
                <Button
                  size="small"
                  variant={v.isLatest ? 'contained' : 'outlined'}
                  startIcon={<UpgradeIcon />}
                  disabled={busy || !v.installable}
                  onClick={() => setConfirmTarget(v)}
                >
                  {v.isCurrent ? t('systemUpdate.remote.reinstall') : t('systemUpdate.remote.upgradeTo', { version: v.version })}
                </Button>
              </Box>
              {v.notes && (
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.5, whiteSpace: 'pre-wrap' }}>
                  {v.notes}
                </Typography>
              )}
              <Typography variant="caption" sx={{ display: 'block', mt: 0.5, color: v.installable ? 'text.secondary' : 'warning.dark' }}>
                {v.files.map((f) => `${f.os}/${f.arch}${f.matched ? ' ✓' : ''}`).join(' · ')}
              </Typography>
            </Box>
          ))}
        </Stack>
      )}

      <ConfirmDialog
        open={!!confirmTarget}
        title={t('systemUpdate.confirm.upgradeTitle')}
        content={t('systemUpdate.confirm.upgradeContent', { version: confirmTarget?.version || t('systemUpdate.remote.latest') })}
        onClose={() => setConfirmTarget(null)}
        onConfirm={handleUpgrade}
      />
      {starting && (
        <Box sx={{ display: 'flex', justifyContent: 'center', mt: 2 }}>
          <CircularProgress size={20} />
        </Box>
      )}
    </MainCard>
  );
}
