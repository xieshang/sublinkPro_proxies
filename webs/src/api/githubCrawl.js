import request from './request';

export function listGitHubCrawlConfigs() {
  return request({ url: '/v1/github-crawl', method: 'get' });
}

export function getGitHubCrawlConfig(id) {
  return request({ url: `/v1/github-crawl/${id}`, method: 'get' });
}

export function createGitHubCrawlConfig(data) {
  return request({ url: '/v1/github-crawl', method: 'post', data });
}

export function updateGitHubCrawlConfig(id, data) {
  return request({ url: `/v1/github-crawl/${id}`, method: 'put', data });
}

export function deleteGitHubCrawlConfig(id) {
  return request({ url: `/v1/github-crawl/${id}`, method: 'delete' });
}

export function toggleGitHubCrawlConfig(id, enabled) {
  return request({ url: `/v1/github-crawl/${id}/toggle`, method: 'post', data: { enabled } });
}

export function runGitHubCrawlNow(id) {
  return request({ url: `/v1/github-crawl/${id}/run`, method: 'post' });
}

export function listGitHubCrawlLogs(id, params = {}) {
  return request({ url: `/v1/github-crawl/${id}/logs`, method: 'get', params });
}

export function clearGitHubCrawlLogs(id) {
  return request({ url: `/v1/github-crawl/${id}/logs`, method: 'delete' });
}

export function listGitHubCrawlRuns(id, params = {}) {
  return request({ url: `/v1/github-crawl/${id}/runs`, method: 'get', params });
}

export function listGitHubCrawlNodes(id, params = {}) {
  return request({ url: `/v1/github-crawl/${id}/nodes`, method: 'get', params });
}

export function clearGitHubCrawlNodes(id) {
  return request({ url: `/v1/github-crawl/${id}/nodes`, method: 'delete' });
}

export function promoteGitHubCrawlNodes(id, nodeIds) {
  return request({ url: `/v1/github-crawl/${id}/promote`, method: 'post', data: { nodeIds }, timeout: 180000 });
}

export function testGitHubCrawlNodeDelay(id, nodeIds) {
  return request({ url: `/v1/github-crawl/${id}/test-delay`, method: 'post', data: { nodeIds } });
}

export function testGitHubCrawlNodeSpeed(id, nodeIds) {
  return request({ url: `/v1/github-crawl/${id}/test-speed`, method: 'post', data: { nodeIds } });
}

export function deleteInvalidGitHubCrawlNodes(id) {
  return request({ url: `/v1/github-crawl/${id}/nodes/invalid`, method: 'delete' });
}

export function deleteGitHubCrawlNodes(id, nodeIds) {
  return request({ url: `/v1/github-crawl/${id}/nodes/delete`, method: 'post', data: { nodeIds } });
}

export function stopGitHubCrawl(id) {
  return request({ url: `/v1/github-crawl/${id}/stop`, method: 'post' });
}

export function testGitHubCrawlNodes(id, nodeIds = [], profileId) {
  return request({
    url: `/v1/github-crawl/${id}/test`,
    method: 'post',
    data: { nodeIds, profileId }
  });
}

