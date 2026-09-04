import request from './request';

// 获取升级状态（当前版本/平台/配置/最近操作）
export function getUpdaterStatus() {
  return request({ url: '/v1/updater/status', method: 'get' });
}

// 获取升级源配置
export function getUpdaterConfig() {
  return request({ url: '/v1/updater/config', method: 'get' });
}

// 保存升级源配置
export function updateUpdaterConfig(data) {
  return request({ url: '/v1/updater/config', method: 'put', data });
}

// 拉取远程版本清单（JSON 版本记录库）
export function listRemoteVersions() {
  return request({ url: '/v1/updater/remote/versions', method: 'get', timeout: 60000 });
}

// 启动升级（version 为空表示安装 latest / 模板直下）
export function startUpgrade(version) {
  return request({ url: '/v1/updater/upgrade', method: 'post', data: { version } });
}

// 本地成品库列表
export function listArtifacts() {
  return request({ url: '/v1/updater/artifacts', method: 'get' });
}

// 回退到指定成品（test 验证 → 替换 → 重启）
export function rollbackArtifact(id) {
  return request({ url: `/v1/updater/artifacts/${id}/rollback`, method: 'post' });
}

// 删除成品（active 不允许删除）
export function deleteArtifact(id) {
  return request({ url: `/v1/updater/artifacts/${id}`, method: 'delete' });
}

// 手动上传成品升级（上传后自动：test 试运行 → 快照 → 替换 → 重启）
export function uploadArtifact(file, version) {
  const form = new FormData();
  form.append('file', file);
  if (version) form.append('version', version);
  return request({
    url: '/v1/updater/upload',
    method: 'post',
    data: form,
    headers: { 'Content-Type': 'multipart/form-data' },
    timeout: 600000
  });
}
