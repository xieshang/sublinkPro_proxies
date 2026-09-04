import { useState, useEffect } from 'react';
import { Trans, useTranslation } from 'react-i18next';

// material-ui
import Button from '@mui/material/Button';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import CardHeader from '@mui/material/CardHeader';
import Divider from '@mui/material/Divider';
import Stack from '@mui/material/Stack';
import Switch from '@mui/material/Switch';
import FormControlLabel from '@mui/material/FormControlLabel';
import TextField from '@mui/material/TextField';
import Typography from '@mui/material/Typography';
import Alert from '@mui/material/Alert';
import Autocomplete from '@mui/material/Autocomplete';

// icons
import SaveIcon from '@mui/icons-material/Save';
import FilterAltIcon from '@mui/icons-material/FilterAlt';
import AutoDeleteIcon from '@mui/icons-material/AutoDelete';

// project imports
import { getNodeDedupConfig, updateNodeDedupConfig } from 'api/settings';
import { getNodeGroups } from 'api/nodes';

// ==============================|| 节点自动处理设置（去重 + 连续失败自动删除） ||============================== //

export default function NodeDedupSettings({ showMessage }) {
  const { t } = useTranslation();
  const [crossAirportDedupEnabled, setCrossAirportDedupEnabled] = useState(true);
  const [landingIPDedupEnabled, setLandingIPDedupEnabled] = useState(false);
  const [autoDeleteEnabled, setAutoDeleteEnabled] = useState(false);
  const [autoDeleteThreshold, setAutoDeleteThreshold] = useState(3);
  const [autoDeleteGroups, setAutoDeleteGroups] = useState([]);
  const [groupOptions, setGroupOptions] = useState([]);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    fetchConfig();
    fetchGroups();
  }, []);

  const fetchConfig = async () => {
    try {
      const res = await getNodeDedupConfig();
      if (res.data) {
        setCrossAirportDedupEnabled(res.data.crossAirportDedupEnabled !== false);
        setLandingIPDedupEnabled(res.data.landingIPDedupEnabled === true);
        setAutoDeleteEnabled(res.data.autoDeleteEnabled === true);
        setAutoDeleteThreshold(Number(res.data.autoDeleteThreshold) || 3);
        setAutoDeleteGroups(Array.isArray(res.data.autoDeleteGroups) ? res.data.autoDeleteGroups : []);
      }
    } catch (error) {
      console.error('获取节点自动处理配置失败:', error);
    }
  };

  const fetchGroups = async () => {
    try {
      const res = await getNodeGroups();
      setGroupOptions(Array.isArray(res.data) ? res.data : []);
    } catch (error) {
      console.error('获取分组列表失败:', error);
    }
  };

  const handleThresholdChange = (e) => {
    const value = e.target.value;
    if (value === '') {
      setAutoDeleteThreshold('');
      return;
    }
    const num = Math.floor(Number(value));
    if (!Number.isNaN(num)) {
      // 输入时放宽展示限制，保存时统一收敛到合法区间
      setAutoDeleteThreshold(num);
    }
  };

  const buildPayload = () => ({
    crossAirportDedupEnabled,
    landingIPDedupEnabled,
    autoDeleteEnabled,
    autoDeleteThreshold: Math.min(20, Math.max(1, Number(autoDeleteThreshold) || 3)),
    autoDeleteGroups
  });

  const handleSave = async () => {
    setSaving(true);
    try {
      await updateNodeDedupConfig(buildPayload());
      showMessage(t('nodeDedup.messages.saveSuccess'));
    } catch (error) {
      console.error('保存失败:', error);
      showMessage(error.message || t('nodeDedup.messages.saveFailed'), 'error');
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card variant="outlined">
      <CardHeader avatar={<FilterAltIcon color="primary" />} title={t('nodeDedup.title')} subheader={t('nodeDedup.subheader')} />
      <CardContent>
        <Stack spacing={2}>
          <FormControlLabel
            control={<Switch checked={crossAirportDedupEnabled} onChange={(e) => setCrossAirportDedupEnabled(e.target.checked)} />}
            label={t('nodeDedup.enable')}
          />
          <Alert severity={crossAirportDedupEnabled ? 'info' : 'warning'} variant="standard">
            <Typography variant="body2">
              {crossAirportDedupEnabled ? (
                <>
                  <Trans i18nKey="nodeDedup.enabledInfo" components={{ strong: <strong /> }} />
                </>
              ) : (
                <>
                  <Trans i18nKey="nodeDedup.disabledInfo" components={{ strong: <strong /> }} />
                </>
              )}
            </Typography>
          </Alert>
          <FormControlLabel
            control={<Switch checked={landingIPDedupEnabled} onChange={(e) => setLandingIPDedupEnabled(e.target.checked)} />}
            label={t('nodeDedup.landingIPEnable')}
          />
          <Alert severity="info" variant="standard">
            <Typography variant="body2">
              <Trans i18nKey="nodeDedup.landingIPInfo" components={{ strong: <strong /> }} />
            </Typography>
          </Alert>

          {/* ========== 连续失败自动删除 ========== */}
          <Divider sx={{ my: 1 }}>
            <Stack direction="row" spacing={1} alignItems="center">
              <AutoDeleteIcon fontSize="small" color="primary" />
              <Typography variant="subtitle2">{t('nodeDedup.autoDelete.sectionTitle')}</Typography>
            </Stack>
          </Divider>
          <FormControlLabel
            control={<Switch checked={autoDeleteEnabled} onChange={(e) => setAutoDeleteEnabled(e.target.checked)} />}
            label={t('nodeDedup.autoDelete.enable')}
          />
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
            <TextField
              type="number"
              size="small"
              sx={{ width: { sm: 220 } }}
              slotProps={{ htmlInput: { min: 1, max: 20 } }}
              disabled={!autoDeleteEnabled}
              label={t('nodeDedup.autoDelete.threshold')}
              value={autoDeleteThreshold}
              onChange={handleThresholdChange}
            />
            <Autocomplete
              multiple
              size="small"
              sx={{ flex: 1 }}
              options={groupOptions}
              value={autoDeleteGroups}
              disabled={!autoDeleteEnabled}
              onChange={(e, newValue) => setAutoDeleteGroups(newValue)}
              renderInput={(params) => (
                <TextField
                  {...params}
                  label={t('nodeDedup.autoDelete.groups')}
                  placeholder={t('nodeDedup.autoDelete.groupsPlaceholder')}
                />
              )}
            />
          </Stack>
          <Alert severity={autoDeleteEnabled ? 'warning' : 'info'} variant="standard">
            <Typography variant="body2">
              <Trans i18nKey="nodeDedup.autoDelete.info" components={{ strong: <strong /> }} />
            </Typography>
          </Alert>

          <Stack direction="row" justifyContent="flex-end">
            <Button variant="contained" startIcon={<SaveIcon />} onClick={handleSave} disabled={saving}>
              {saving ? t('common.saving') : t('common.save')}
            </Button>
          </Stack>
        </Stack>
      </CardContent>
    </Card>
  );
}
