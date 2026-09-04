import { useTranslation } from 'react-i18next';

import { alpha, useTheme } from '@mui/material/styles';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import Divider from '@mui/material/Divider';
import Skeleton from '@mui/material/Skeleton';
import Stack from '@mui/material/Stack';
import Typography from '@mui/material/Typography';

import RefreshIcon from '@mui/icons-material/Refresh';
import RocketLaunchIcon from '@mui/icons-material/RocketLaunch';

import MainCard from 'ui-component/cards/MainCard';

const InfoItem = ({ label, value, mono = false }) => (
  <Box sx={{ minWidth: 0 }}>
    <Typography variant="caption" color="text.secondary">
      {label}
    </Typography>
    <Typography
      variant="body2"
      sx={{ fontWeight: 600, wordBreak: 'break-all', fontFamily: mono ? '"JetBrains Mono", monospace' : undefined }}
    >
      {value}
    </Typography>
  </Box>
);

export default function UpgradeStatusCard({ status, onRefresh, onNotify }) {
  const theme = useTheme();
  const { t } = useTranslation();

  if (!status) {
    return (
      <MainCard title={t('systemUpdate.status.title')} sx={{ mb: 3 }}>
        <Stack spacing={1}>
          <Skeleton height={24} />
          <Skeleton height={24} width="70%" />
        </Stack>
      </MainCard>
    );
  }

  const busy = !!status.busy;
  const lastOp = status.lastOperation;

  const handleRefresh = async () => {
    try {
      await onRefresh();
    } catch {
      onNotify?.(t('systemUpdate.messages.loadStatusFailed'), 'error');
    }
  };

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
              backgroundColor: alpha(theme.palette.primary.main, 0.12),
              border: `1px solid ${alpha(theme.palette.primary.main, 0.28)}`
            }}
          >
            <RocketLaunchIcon fontSize="small" sx={{ color: theme.palette.primary.main }} />
          </Box>
          {t('systemUpdate.status.title')}
        </Box>
      }
      secondary={
        <Button size="small" startIcon={<RefreshIcon />} onClick={handleRefresh} disabled={busy}>
          {t('systemUpdate.status.refresh')}
        </Button>
      }
      sx={{ mb: 3 }}
    >
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: { xs: '1fr 1fr', sm: 'repeat(4, minmax(0, 1fr))' },
          gap: 2
        }}
      >
        <InfoItem label={t('systemUpdate.status.currentVersion')} value={status.version || '-'} mono />
        <InfoItem label={t('systemUpdate.status.platform')} value={status.platform ? `${status.platform.os}/${status.platform.arch}` : '-'} mono />
        <InfoItem label={t('systemUpdate.status.exePath')} value={status.exePath || '-'} mono />
        <Box>
          <Typography variant="caption" color="text.secondary">
            {t('systemUpdate.status.state')}
          </Typography>
          <Box sx={{ mt: 0.5 }}>
            <Chip
              size="small"
              color={busy ? 'warning' : 'success'}
              label={busy ? t('systemUpdate.status.busy') : t('systemUpdate.status.idle')}
            />
          </Box>
        </Box>
      </Box>

      {lastOp && (
        <>
          <Divider sx={{ my: 2 }} />
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
            <Chip
              size="small"
              color={lastOp.status === 'success' ? 'success' : lastOp.status === 'failed' ? 'error' : 'warning'}
              label={t(`systemUpdate.status.op.${lastOp.status}`, lastOp.status)}
            />
            <Typography variant="caption" color="text.secondary">
              {lastOp.action === 'rollback' ? t('systemUpdate.status.rollbackAction') : t('systemUpdate.status.upgradeAction')}
              {lastOp.version ? ` · ${lastOp.version}` : ''}
            </Typography>
            <Typography variant="caption" sx={{ color: lastOp.status === 'failed' ? 'error.main' : 'text.secondary', flex: 1, minWidth: 120 }}>
              {lastOp.message}
            </Typography>
          </Box>
        </>
      )}
    </MainCard>
  );
}
