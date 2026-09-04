import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';

import Box from '@mui/material/Box';
import Snackbar from '@mui/material/Snackbar';
import Alert from '@mui/material/Alert';

import { getUpdaterStatus } from 'api/updater';

import { ReleaseLogPanel, StarReminderCard } from 'components/SystemUpdatePanels';
import UpgradeSourceCard from './component/UpgradeSourceCard';
import UpgradeStatusCard from './component/UpgradeStatusCard';
import RemoteVersionsPanel from './component/RemoteVersionsPanel';
import ArtifactLibraryCard from './component/ArtifactLibraryCard';

const STATUS_POLL_MS = 3000;

export default function SystemUpdatesPage() {
  const { t } = useTranslation();
  const [snackbar, setSnackbar] = useState({ open: false, message: '', severity: 'success' });
  const [status, setStatus] = useState(null);
  const pollRef = useRef(null);

  const showSnackbar = useCallback((message, severity = 'success') => {
    setSnackbar({ open: true, message, severity });
  }, []);

  const refreshStatus = useCallback(async () => {
    try {
      const res = await getUpdaterStatus();
      setStatus(res?.data || null);
    } catch (error) {
      console.error(t('systemUpdate.messages.loadStatusFailed'), error);
    }
  }, [t]);

  useEffect(() => {
    refreshStatus();
    pollRef.current = setInterval(refreshStatus, STATUS_POLL_MS);
    return () => clearInterval(pollRef.current);
  }, [refreshStatus]);

  const busy = !!status?.busy;

  const notify = useCallback(
    (message, severity = 'success') => showSnackbar(message, severity),
    [showSnackbar]
  );

  return (
    <Box sx={{ pb: 3 }}>
      <UpgradeStatusCard status={status} onRefresh={refreshStatus} onNotify={notify} />
      <UpgradeSourceCard config={status?.config} busy={busy} onNotify={notify} />
      <RemoteVersionsPanel busy={busy} onNotify={notify} onStarted={refreshStatus} />
      <ArtifactLibraryCard busy={busy} onNotify={notify} onStarted={refreshStatus} />
      <StarReminderCard />
      <ReleaseLogPanel />

      <Snackbar
        open={snackbar.open}
        autoHideDuration={4000}
        onClose={() => setSnackbar((prev) => ({ ...prev, open: false }))}
        anchorOrigin={{ vertical: 'top', horizontal: 'center' }}
      >
        <Alert severity={snackbar.severity} variant="filled" onClose={() => setSnackbar((prev) => ({ ...prev, open: false }))}>
          {snackbar.message}
        </Alert>
      </Snackbar>
    </Box>
  );
}
