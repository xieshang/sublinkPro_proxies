import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { alpha, useTheme } from '@mui/material/styles';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import Dialog from '@mui/material/Dialog';
import DialogActions from '@mui/material/DialogActions';
import DialogContent from '@mui/material/DialogContent';
import DialogTitle from '@mui/material/DialogTitle';
import IconButton from '@mui/material/IconButton';
import Stack from '@mui/material/Stack';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import TextField from '@mui/material/TextField';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';

import HistoryIcon from '@mui/icons-material/History';
import DeleteOutlineIcon from '@mui/icons-material/DeleteOutline';
import UndoIcon from '@mui/icons-material/Undo';
import UploadFileIcon from '@mui/icons-material/UploadFile';

import MainCard from 'ui-component/cards/MainCard';
import ConfirmDialog from 'components/ConfirmDialog';
import { formatDateTime } from 'i18n/locales';
import { formatBytes } from 'views/airports/utils';
import { deleteArtifact, listArtifacts, rollbackArtifact, uploadArtifact } from 'api/updater';

export default function ArtifactLibraryCard({ busy, onNotify, onStarted }) {
  const theme = useTheme();
  const { t, i18n } = useTranslation();
  const [artifacts, setArtifacts] = useState([]);
  const [currentVersion, setCurrentVersion] = useState('');
  const [confirmRollback, setConfirmRollback] = useState(null);
  const [confirmDelete, setConfirmDelete] = useState(null);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [uploadFile, setUploadFile] = useState(null);
  const [uploadVersion, setUploadVersion] = useState('');
  const [uploading, setUploading] = useState(false);
  const fileInputRef = useRef(null);

  const load = useCallback(async () => {
    try {
      const res = await listArtifacts();
      setArtifacts(res?.data?.artifacts || []);
      setCurrentVersion(res?.data?.current || '');
    } catch (error) {
      onNotify?.(error?.message || t('systemUpdate.artifacts.loadFailed'), 'error');
    }
  }, [onNotify, t]);

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleRollback = async () => {
    if (!confirmRollback) return;
    try {
      await rollbackArtifact(confirmRollback.id);
      onNotify?.(t('systemUpdate.messages.rollbackStarted', { version: confirmRollback.version }));
      setConfirmRollback(null);
      onStarted?.();
    } catch (error) {
      onNotify?.(error?.message || t('systemUpdate.messages.rollbackStartFailed'), 'error');
    }
  };

  const handleDelete = async () => {
    if (!confirmDelete) return;
    try {
      await deleteArtifact(confirmDelete.id);
      onNotify?.(t('systemUpdate.messages.deleteSuccess'));
      setConfirmDelete(null);
      load();
    } catch (error) {
      onNotify?.(error?.message || t('systemUpdate.messages.deleteFailed'), 'error');
    }
  };

  const handleUpload = async () => {
    if (!uploadFile) return;
    try {
      setUploading(true);
      await uploadArtifact(uploadFile, String(uploadVersion).trim());
      onNotify?.(t('systemUpdate.messages.uploadStarted', { file: uploadFile.name }));
      setUploadOpen(false);
      setUploadFile(null);
      setUploadVersion('');
      onStarted?.();
    } catch (error) {
      onNotify?.(error?.message || t('systemUpdate.messages.uploadFailed'), 'error');
    } finally {
      setUploading(false);
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
              backgroundColor: alpha(theme.palette.warning.main, theme.palette.mode === 'dark' ? 0.18 : 0.12),
              border: `1px solid ${alpha(theme.palette.warning.main, theme.palette.mode === 'dark' ? 0.32 : 0.2)}`
            }}
          >
            <HistoryIcon fontSize="small" sx={{ color: theme.palette.warning.dark }} />
          </Box>
          {t('systemUpdate.artifacts.title')}
        </Box>
      }
      action={
        <Button
          size="small"
          variant="outlined"
          startIcon={<UploadFileIcon />}
          disabled={busy || uploading}
          onClick={() => setUploadOpen(true)}
        >
          {t('systemUpdate.artifacts.upload')}
        </Button>
      }
      sx={{ mb: 3 }}
    >
      <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
        {t('systemUpdate.artifacts.subtitle')}
      </Typography>
      {artifacts.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          {t('systemUpdate.artifacts.empty')}
        </Typography>
      ) : (
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>{t('systemUpdate.artifacts.version')}</TableCell>
                <TableCell>{t('systemUpdate.artifacts.platform')}</TableCell>
                <TableCell>{t('systemUpdate.artifacts.size')}</TableCell>
                <TableCell>{t('systemUpdate.artifacts.status')}</TableCell>
                <TableCell>{t('systemUpdate.artifacts.testedAt')}</TableCell>
                <TableCell align="right">{t('systemUpdate.artifacts.actions')}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {artifacts.map((a) => (
                <TableRow key={a.id} hover>
                  <TableCell>
                    <Stack direction="row" spacing={1} alignItems="center">
                      <Typography variant="body2" sx={{ fontWeight: 600 }}>
                        {a.version}
                      </Typography>
                      {a.status === 'active' && <Chip size="small" color="success" label={t('systemUpdate.artifacts.active')} />}
                    </Stack>
                  </TableCell>
                  <TableCell>
                    <Typography variant="caption" sx={{ fontFamily: '"JetBrains Mono", monospace' }}>
                      {a.os}/{a.arch}
                    </Typography>
                  </TableCell>
                  <TableCell>
                    <Typography variant="caption">{a.size ? formatBytes(a.size) : '-'}</Typography>
                  </TableCell>
                  <TableCell>
                    <Chip
                      size="small"
                      variant="outlined"
                      color={a.status === 'active' ? 'success' : a.status === 'backup' ? 'default' : 'info'}
                      label={t(`systemUpdate.artifacts.${a.status}`, a.status)}
                    />
                  </TableCell>
                  <TableCell>
                    <Tooltip title={t('systemUpdate.artifacts.testedAtTip')}>
                      <Typography variant="caption">{formatDateTime(a.testedAt, i18n.resolvedLanguage || i18n.language)}</Typography>
                    </Tooltip>
                  </TableCell>
                  <TableCell align="right">
                    {a.status !== 'active' && (
                      <Stack direction="row" spacing={0.5} justifyContent="flex-end">
                        <Tooltip title={t('systemUpdate.artifacts.rollback')}>
                          <span>
                            <IconButton size="small" color="primary" disabled={busy} onClick={() => setConfirmRollback(a)}>
                              <UndoIcon fontSize="small" />
                            </IconButton>
                          </span>
                        </Tooltip>
                        <Tooltip title={t('systemUpdate.artifacts.delete')}>
                          <span>
                            <IconButton size="small" color="error" disabled={busy} onClick={() => setConfirmDelete(a)}>
                              <DeleteOutlineIcon fontSize="small" />
                            </IconButton>
                          </span>
                        </Tooltip>
                      </Stack>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      )}

      <ConfirmDialog
        open={!!confirmRollback}
        title={t('systemUpdate.confirm.rollbackTitle')}
        content={
          confirmRollback
            ? t('systemUpdate.confirm.rollbackContent', {
                version: confirmRollback.version,
                current: currentVersion || '-'
              })
            : ''
        }
        onClose={() => setConfirmRollback(null)}
        onConfirm={handleRollback}
      />
      <ConfirmDialog
        open={!!confirmDelete}
        title={t('systemUpdate.confirm.deleteTitle')}
        content={t('systemUpdate.confirm.deleteContent', { version: confirmDelete?.version })}
        onClose={() => setConfirmDelete(null)}
        onConfirm={handleDelete}
      />

      <Dialog open={uploadOpen} onClose={() => !uploading && setUploadOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>{t('systemUpdate.artifacts.upload')}</DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ mt: 0.5 }}>
            <Typography variant="caption" color="text.secondary">
              {t('systemUpdate.artifacts.uploadTip')}
            </Typography>
            <Box>
              <input
                ref={fileInputRef}
                type="file"
                accept=".exe,.zip,.gz,.tgz,.tar.gz,application/zip,application/gzip,application/octet-stream"
                style={{ display: 'none' }}
                onChange={(e) => setUploadFile(e.target.files?.[0] || null)}
              />
              <Button variant="outlined" fullWidth onClick={() => fileInputRef.current?.click()} disabled={uploading}>
                {uploadFile ? uploadFile.name : t('systemUpdate.artifacts.uploadChoose')}
              </Button>
              {uploadFile && (
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.5 }}>
                  {formatBytes(uploadFile.size)}
                </Typography>
              )}
            </Box>
            <TextField
              size="small"
              label={t('systemUpdate.artifacts.uploadVersion')}
              placeholder="v1.2.3"
              value={uploadVersion}
              onChange={(e) => setUploadVersion(e.target.value)}
              helperText={t('systemUpdate.artifacts.uploadVersionHelper')}
              disabled={uploading}
            />
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setUploadOpen(false)} disabled={uploading}>
            {t('systemUpdate.artifacts.uploadCancel')}
          </Button>
          <Button variant="contained" disabled={!uploadFile || uploading || busy} onClick={handleUpload}>
            {uploading ? t('systemUpdate.artifacts.uploadUploading') : t('systemUpdate.artifacts.uploadConfirm')}
          </Button>
        </DialogActions>
      </Dialog>
    </MainCard>
  );
}
