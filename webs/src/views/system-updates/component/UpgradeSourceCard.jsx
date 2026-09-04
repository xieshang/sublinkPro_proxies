import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { alpha, useTheme } from '@mui/material/styles';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import FormControl from '@mui/material/FormControl';
import FormControlLabel from '@mui/material/FormControlLabel';
import FormLabel from '@mui/material/FormLabel';
import MenuItem from '@mui/material/MenuItem';
import Radio from '@mui/material/Radio';
import RadioGroup from '@mui/material/RadioGroup';
import TextField from '@mui/material/TextField';
import Typography from '@mui/material/Typography';

import CloudDownloadIcon from '@mui/icons-material/CloudDownload';
import SaveIcon from '@mui/icons-material/Save';

import MainCard from 'ui-component/cards/MainCard';
import { updateUpdaterConfig } from 'api/updater';

export default function UpgradeSourceCard({ config, busy, onNotify }) {
  const theme = useTheme();
  const { t } = useTranslation();
  const [form, setForm] = useState({
    sourceType: 'manifest',
    manifestUrl: '',
    templateUrl: '',
    githubRepo: '',
    githubToken: '',
    useProxy: false,
    keepArtifacts: 10
  });
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (config) {
      setForm((prev) => ({
        ...prev,
        sourceType: config.sourceType || 'manifest',
        manifestUrl: config.manifestUrl || '',
        templateUrl: config.templateUrl || '',
        githubRepo: config.githubRepo || '',
        // token 出于安全不回显，仅在用户输入时覆盖
        githubToken: '',
        useProxy: !!config.useProxy,
        keepArtifacts: config.keepArtifacts ?? 10
      }));
    }
  }, [config]);

  const canEdit = !busy && !saving;

  const handleSave = async () => {
    if (!String(form.manifestUrl).trim() && form.sourceType === 'manifest') {
      onNotify?.(t('systemUpdate.source.errors.emptyManifestUrl'), 'error');
      return;
    }
    if (!String(form.templateUrl).trim() && form.sourceType === 'template') {
      onNotify?.(t('systemUpdate.source.errors.emptyTemplateUrl'), 'error');
      return;
    }
    if (form.sourceType === 'github' && !/^[^\s/]+\/[^\s/]+$/.test(String(form.githubRepo).trim())) {
      onNotify?.(t('systemUpdate.source.errors.invalidGitHubRepo'), 'error');
      return;
    }
    try {
      setSaving(true);
      await updateUpdaterConfig({
        ...form,
        manifestUrl: String(form.manifestUrl).trim(),
        templateUrl: String(form.templateUrl).trim(),
        githubRepo: String(form.githubRepo).trim(),
        keepArtifacts: Number(form.keepArtifacts) || 10
      });
      onNotify?.(t('systemUpdate.messages.configSaved'));
    } catch (error) {
      onNotify?.(error?.message || t('systemUpdate.messages.configSaveFailed'), 'error');
    } finally {
      setSaving(false);
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
              backgroundColor: alpha(theme.palette.secondary.main, theme.palette.mode === 'dark' ? 0.18 : 0.1),
              border: `1px solid ${alpha(theme.palette.secondary.main, theme.palette.mode === 'dark' ? 0.32 : 0.18)}`
            }}
          >
            <CloudDownloadIcon fontSize="small" sx={{ color: theme.palette.secondary.main }} />
          </Box>
          {t('systemUpdate.source.title')}
        </Box>
      }
      sx={{ mb: 3 }}
    >
      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2.5 }}>
        <FormControl component="fieldset" disabled={!canEdit}>
          <FormLabel component="legend" sx={{ fontSize: '0.875rem' }}>
            {t('systemUpdate.source.sourceType')}
          </FormLabel>
          <RadioGroup
            row
            value={form.sourceType}
            onChange={(e) => setForm({ ...form, sourceType: e.target.value })}
          >
            <FormControlLabel value="manifest" control={<Radio size="small" />} label={t('systemUpdate.source.typeManifest')} />
            <FormControlLabel value="template" control={<Radio size="small" />} label={t('systemUpdate.source.typeTemplate')} />
            <FormControlLabel value="github" control={<Radio size="small" />} label={t('systemUpdate.source.typeGitHub')} />
          </RadioGroup>
          <Typography variant="caption" color="text.secondary">
            {form.sourceType === 'manifest'
              ? t('systemUpdate.source.typeManifestTip')
              : form.sourceType === 'github'
                ? t('systemUpdate.source.typeGitHubTip')
                : t('systemUpdate.source.typeTemplateTip')}
          </Typography>
        </FormControl>

        {form.sourceType === 'manifest' ? (
          <TextField
            size="small"
            fullWidth
            disabled={!canEdit}
            label={t('systemUpdate.source.manifestUrl')}
            placeholder="https://example.com/sublinkpro/versions.json"
            value={form.manifestUrl}
            onChange={(e) => setForm({ ...form, manifestUrl: e.target.value })}
          />
        ) : form.sourceType === 'github' ? (
          <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr' }, gap: 2 }}>
            <TextField
              size="small"
              disabled={!canEdit}
              label={t('systemUpdate.source.githubRepo')}
              placeholder="owner/repo"
              value={form.githubRepo}
              onChange={(e) => setForm({ ...form, githubRepo: e.target.value })}
            />
            <TextField
              size="small"
              type="password"
              disabled={!canEdit}
              label={t('systemUpdate.source.githubToken')}
              placeholder={config?.githubToken ? t('systemUpdate.source.githubTokenSet') : 'ghp_…'}
              value={form.githubToken}
              onChange={(e) => setForm({ ...form, githubToken: e.target.value })}
              helperText={t('systemUpdate.source.githubTokenHelper')}
            />
          </Box>
        ) : (
          <TextField
            size="small"
            fullWidth
            disabled={!canEdit}
            label={t('systemUpdate.source.templateUrl')}
            placeholder="https://example.com/dl/{version}/sublink_{os}_{arch}{ext}"
            value={form.templateUrl}
            onChange={(e) => setForm({ ...form, templateUrl: e.target.value })}
            helperText={t('systemUpdate.source.templateHelper')}
          />
        )}

        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr' }, gap: 2 }}>
          <TextField
            select
            size="small"
            disabled={!canEdit}
            label={t('systemUpdate.source.useProxy')}
            value={form.useProxy ? 'yes' : 'no'}
            onChange={(e) => setForm({ ...form, useProxy: e.target.value === 'yes' })}
          >
            <MenuItem value="no">{t('systemUpdate.source.proxyDirect')}</MenuItem>
            <MenuItem value="yes">{t('systemUpdate.source.proxyMihomo')}</MenuItem>
          </TextField>
          <TextField
            select
            size="small"
            disabled={!canEdit}
            label={t('systemUpdate.source.keepArtifacts')}
            value={String(form.keepArtifacts)}
            onChange={(e) => setForm({ ...form, keepArtifacts: Number(e.target.value) })}
            SelectProps={{ MenuProps: { PaperProps: { style: { maxHeight: 220 } } } }}
          >
            {[3, 5, 8, 10, 15, 20].map((n) => (
              <MenuItem key={n} value={String(n)}>
                {t('systemUpdate.source.keepN', { count: n })}
              </MenuItem>
            ))}
          </TextField>
        </Box>

        <Box>
          <Button variant="contained" startIcon={<SaveIcon />} onClick={handleSave} disabled={!canEdit}>
            {saving ? t('systemUpdate.source.saving') : t('systemUpdate.source.save')}
          </Button>
        </Box>

        <Typography variant="caption" color="text.secondary">
          {t('systemUpdate.source.formatHint')}
        </Typography>
      </Box>
    </MainCard>
  );
}
