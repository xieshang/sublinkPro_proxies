package node

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"sublink/models"
	"sublink/node/protocol"
	"sublink/services/mihomo"
	"sublink/utils"
)

const (
	githubAPIBase         = "https://api.github.com"
	githubSearchPageSize  = 10
	githubMaxRepos        = 100 // 单次仓库搜索上限（非有效入库目标）
	githubMaxSubURLs      = 500 // 安全上限；真正停止条件是有效入库数
	githubMaxSubPerRepo   = 3   // 同一仓库最多提取 3 个文件
	githubMaxNodesTotal   = 2000
	githubMaxNodesPerSub  = 300
	githubMaxTestPerSub   = 40
	githubTestConcurrency = 5
	githubMaxREADMEBytes  = 512 << 10
	githubMaxContentBytes = 2 << 20
	githubHTTPTimeout     = 8 * time.Second
	githubSubFetchTimeout = 6 * time.Second
	githubProbeTimeout    = 3 * time.Second
	githubDefaultMaxLinks = 40 // MaxCrawlLinks 默认：目标有效入库数
	githubMaxDirectLinks  = 40
	githubCommitLookback  = 12
	githubMaxFilesPerRepo = 3 // 同一仓库最多提取/测速 3 个文件
)

var (
	proxyLinkPattern = regexp.MustCompile(`(?i)(?:ss|ssr|vmess|vless|trojan|hysteria2?|hy2|tuic|anytls|snell|mieru|socks5?)://[^\s"'<>\]]+`)
	httpURLPattern   = regexp.MustCompile(`https?://[^\s"'<>\])},]+`)

	subHintPattern    = regexp.MustCompile(`(?i)(raw\.githubusercontent\.com|gist\.githubusercontent\.com|subscribe|sub\b|clash|proxies|/ya?ml|base64|v2ray|vmess|trojan|ssr?://|hysteria|hy2|tuic|sing-box|mihomo|proxy)`)
	subExcludePattern = regexp.MustCompile(`(?i)(` +
		`github\.com/[^/]+/[^/]+/(?:issues|pull|commit|actions|wiki|projects|security|settings|tree|blob)(?:/|$)|` +
		`github\.com/[^/]+/[^/]+/?$|` +
		`/releases(?:/download)?(?:/|$)|` +
		`\.(?:png|jpe?g|gif|svg|webp|ico|css|js|map|mp4|zip|gz|tgz|rar|7z|exe|dmg|deb|rpm|apk|html?)(?:\?|$)|` +
		`shields\.io|img\.shields|badge|buymeacoffee|paypal\.me|afdian|` +
		`raw\.githubusercontent\.com/.+/(?:LICENSE(?:\..+)?|README(?:\..+)?|CHANGELOG(?:\..+)?|NEWS(?:\..+)?|TODO(?:\..+)?|.*\.md)$` +
		`)`)
	clashURLPattern = regexp.MustCompile(`(?i)(clash|mihomo|openclash|proxies\.ya?ml|proxy-providers|flag=clash|target=clash|client=clash|type=clash|format=clash|/clash(?:/|\.|$|\?)|\.ya?ml(?:\?|$))`)
	weakSubPattern  = regexp.MustCompile(`(?i)(/api/v1/client/subscribe|/sub\?(?:token|uuid)=|[?&](?:token|uuid)=[^&]+$|/subscribe(?:\?|$)|/get\?token=|base64)`)
	nonClashPattern = regexp.MustCompile(`(?i)(sing-?box|v2rayn|v2rayng|quantumult|surge|loon|shadowrocket|target=sing|target=v2ray|target=surge|client=v2ray|flag=singbox|format=v2ray)`)

	featurePathPattern = regexp.MustCompile(`(?i)(clash|mihomo|meta|openclash|proxies|proxy|proxy.?pool|subscribe|subscription|sub\b|nodes?|airport|hysteria|vless|vmess|trojan|ssr?|sing.?box|free.?node|yaml|yml)`)
	excludePathPattern = regexp.MustCompile(`(?i)(^|/|\\)(license|readme|changelog|news|todo|contributing|code_of_conduct|security|authors?|copying|history|changes?)(\.[a-z0-9]+)?$|\.(go|py|js|ts|tsx|jsx|java|c|cpp|h|rs|rb|php|cs|swift|kt|gradle|lock|sum)$`)

	commonSubPaths = []string{
		"clash.yaml", "clash.yml", "config.yaml", "config.yml",
		"subscription.yaml", "subscription.yml", "proxies.yaml", "proxies.yml",
		"nodes.yaml", "nodes.yml", "meta.yaml", "meta.yml",
		"clash/config.yaml", "clash/config.yml", "subscribe.txt", "sub.txt",
		"v2ray.txt", "ss.txt", "links.txt", "free.yaml", "free.yml",
		"clash/proxies.yaml", "provider.yaml", "provider.yml",
	}

	defaultGitHubSearchQueries = []string{
		"mihomo free nodes yaml",
		"clash meta free subscription",
		"clash free subscription",
		"hysteria2 free clash",
		"free proxy pool clash yaml",
		"daily free nodes clash",
		"proxypool free clash",
		"mihomo free clash yaml",
	}

	lastGitHubSearch   = make(map[int]time.Time)
	lastGitHubSearchMu sync.Mutex
)

type githubRepoRef struct {
	FullName      string
	Owner         string
	Name          string
	HTMLURL       string
	Description   string
	DefaultBranch string
	UpdatedAt     string
	PushedAt      string
	Recency       time.Time
}

type githubFileCandidate struct {
	URL       string
	Repo      string
	Path      string
	SHA       string
	Source    string
	Score     int
	UpdatedAt time.Time
}

type githubRepoSearchResponse struct {
	TotalCount int `json:"total_count"`
	Items      []struct {
		FullName      string `json:"full_name"`
		HTMLURL       string `json:"html_url"`
		Description   string `json:"description"`
		DefaultBranch string `json:"default_branch"`
		UpdatedAt     string `json:"updated_at"`
		PushedAt      string `json:"pushed_at"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	} `json:"items"`
}

type githubCommitListItem struct {
	SHA    string `json:"sha"`
	Commit struct {
		Committer struct {
			Date string `json:"date"`
		} `json:"committer"`
		Author struct {
			Date string `json:"date"`
		} `json:"author"`
		Message string `json:"message"`
	} `json:"commit"`
}

type githubCommitDetail struct {
	SHA   string `json:"sha"`
	Files []struct {
		Filename string `json:"filename"`
		Status   string `json:"status"`
		RawURL   string `json:"raw_url"`
	} `json:"files"`
	Commit struct {
		Committer struct {
			Date string `json:"date"`
		} `json:"committer"`
		Author struct {
			Date string `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

type githubContentResponse struct {
	Type     string `json:"type"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
	SHA      string `json:"sha"`
	Path     string `json:"path"`
	Size     int    `json:"size"`
	Download string `json:"download_url"`
}

type githubCodeSearchResponse struct {
	TotalCount int `json:"total_count"`
	Items      []struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		SHA        string `json:"sha"`
		HTMLURL    string `json:"html_url"`
		Repository struct {
			FullName      string `json:"full_name"`
			DefaultBranch string `json:"default_branch"`
			PushedAt      string `json:"pushed_at"`
			UpdatedAt     string `json:"updated_at"`
		} `json:"repository"`
	} `json:"items"`
}

type GitHubLogFn func(level, message string)

type GitHubCrawlResult struct {
	RunID        int
	FilesScanned int
	NodesFound   int
	NodesAdded   int
	Skipped      bool
	SkipReason   string
	Message      string
}

func CrawlGitHubNodes(ctx context.Context, cfg *models.GitHubCrawlConfig, runID int, logFn GitHubLogFn, reporter TaskReporter) (*GitHubCrawlResult, error) {
	if cfg == nil {
		return nil, fmt.Errorf("GitHub crawl config is empty")
	}
	if reporter == nil {
		reporter = &NoOpTaskReporter{}
	}
	if logFn == nil {
		logFn = func(string, string) {}
	}

	result := &GitHubCrawlResult{RunID: runID}
	keywords := enhanceSearchKeywords(splitSearchKeywords(cfg.SearchKeywords))
	if len(keywords) == 0 {
		keywords = append([]string{}, defaultGitHubSearchQueries...)
	}

	shouldSearch := true
	lastGitHubSearchMu.Lock()
	if last, ok := lastGitHubSearch[cfg.ID]; ok && cfg.SearchInterval > 0 {
		if time.Since(last) < time.Duration(cfg.SearchInterval)*time.Second {
			shouldSearch = false
		}
	}
	lastGitHubSearchMu.Unlock()

	if !shouldSearch {
		msg := fmt.Sprintf("搜索间隔未到（%ds），跳过", cfg.SearchInterval)
		logFn("info", msg)
		utils.Info("[GitHubCrawl] config[%s] %s", cfg.Name, msg)
		reporter.ReportComplete(msg, map[string]any{"skipped": true, "reason": "search_interval"})
		result.Skipped = true
		result.SkipReason = "search_interval"
		result.Message = msg
		return result, nil
	}

	token := strings.TrimSpace(cfg.GitHubToken)
	if token == "" {
		logFn("warn", "未配置 GitHub Token，仓库搜索可能被严格限流（建议配置 Personal Access Token）")
	}

	// MaxCrawlLinks = 目标有效入库节点数
	targetValid := cfg.MaxCrawlLinks
	if targetValid <= 0 {
		targetValid = githubDefaultMaxLinks
	}
	if targetValid > 500 {
		targetValid = 500
	}

	logFn("info", fmt.Sprintf(
		"开始抓取，关键字: %v；目标有效入库 %d；同一仓库最多提取 %d 个文件；多关键词搜索后按更新时间排序，最新优先",
		keywords, targetValid, githubMaxFilesPerRepo,
	))
	utils.Info("[GitHubCrawl] start config[%s] keywords=%v targetValid=%d", cfg.Name, keywords, targetValid)

	client := &http.Client{Timeout: githubHTTPTimeout}
	if cfg.UseProxy {
		proxyClient, usedProxy, err := utils.CreateProxyHTTPClient(true, "", githubHTTPTimeout)
		if err != nil {
			logFn("warn", "创建代理客户端失败，将直连: "+err.Error())
		} else {
			client = proxyClient
			if usedProxy != "" {
				logFn("info", "拉取将尝试使用代理节点")
			} else {
				logFn("info", "已开启代理拉取，但未找到可用代理，将直连")
			}
		}
	} else {
		logFn("info", "拉取使用直连（未启用代理）")
	}

	// 仓库搜索数量与有效入库目标解耦
	repoSearchLimit := targetValid * 5
	if repoSearchLimit < 30 {
		repoSearchLimit = 30
	}
	if repoSearchLimit > githubMaxRepos {
		repoSearchLimit = githubMaxRepos
	}

	repos, err := searchGitHubRepos(ctx, client, token, keywords, repoSearchLimit, logFn)
	if err != nil {
		logFn("error", err.Error())
		return nil, err
	}

	codeCands, codeErr := searchGitHubFeatureFiles(ctx, client, token, keywords, repoSearchLimit, logFn)
	if codeErr != nil {
		logFn("warn", "特征文件代码搜索失败: "+codeErr.Error())
	}

	if len(repos) == 0 && len(codeCands) == 0 {
		msg := "未找到匹配的 GitHub 仓库/特征文件，请调整关键字"
		logFn("warn", msg)
		return nil, fmt.Errorf("%s", msg)
	}

	lastGitHubSearchMu.Lock()
	lastGitHubSearch[cfg.ID] = time.Now()
	lastGitHubSearchMu.Unlock()

	// 按更新时间降序排序仓库（最新的在前面）
	sort.SliceStable(repos, func(i, j int) bool {
		return repos[i].Recency.After(repos[j].Recency)
	})

	// 将代码搜索结果按仓库分组
	codeByRepo := make(map[string][]githubFileCandidate)
	orphanCode := make([]githubFileCandidate, 0)
	repoIndex := make(map[string]struct{}, len(repos))
	for _, r := range repos {
		repoIndex[strings.ToLower(r.FullName)] = struct{}{}
	}
	for _, c := range codeCands {
		repoName := strings.TrimSpace(c.Repo)
		if repoName == "" {
			orphanCode = append(orphanCode, c)
			continue
		}
		key := strings.ToLower(repoName)
		codeByRepo[key] = append(codeByRepo[key], c)
		if _, ok := repoIndex[key]; !ok {
			owner, name := "", ""
			if parts := strings.SplitN(repoName, "/", 2); len(parts) == 2 {
				owner, name = parts[0], parts[1]
			}
			repos = append(repos, githubRepoRef{
				FullName:      repoName,
				Owner:         owner,
				Name:          name,
				DefaultBranch: "main",
				UpdatedAt:     c.UpdatedAt.UTC().Format(time.RFC3339),
				PushedAt:      c.UpdatedAt.UTC().Format(time.RFC3339),
				Recency:       c.UpdatedAt,
			})
			repoIndex[key] = struct{}{}
		}
	}

	// 再次按更新时间排序（合并后的列表）
	sort.SliceStable(repos, func(i, j int) bool {
		return repos[i].Recency.After(repos[j].Recency)
	})

	logFn("info", fmt.Sprintf(
		"候选仓库 %d 个（已按更新时间降序）、代码特征文件 %d 个；开始逐库扫描（目标有效 %d）",
		len(repos), len(codeCands), targetValid,
	))
	reporter.UpdateTotal(targetValid)

	var allProxies []protocol.Proxy
	seenLinks := make(map[string]struct{})
	seenFileURL := make(map[string]struct{})
	fetchedSubs := 0
	validCount := 0
	filesScanned := 0
	reposScanned := 0

	// processCandidates 对候选文件测速入库，返回是否已达目标
	processCandidates := func(repoName string, cands []githubFileCandidate) bool {
		if len(cands) == 0 {
			return validCount >= targetValid
		}
		sort.SliceStable(cands, func(i, j int) bool {
			if cands[i].Score != cands[j].Score {
				return cands[i].Score > cands[j].Score
			}
			return cands[i].UpdatedAt.After(cands[j].UpdatedAt)
		})
		if len(cands) > githubMaxFilesPerRepo {
			cands = cands[:githubMaxFilesPerRepo]
		}
		logFn("info", fmt.Sprintf("仓库 %s 选取 %d 个文件进行提取（同库上限 %d）",
			repoName, len(cands), githubMaxFilesPerRepo))

		for _, cand := range cands {
			select {
			case <-ctx.Done():
				return true
			default:
			}
			if validCount >= targetValid {
				return true
			}
			if fetchedSubs >= githubMaxSubURLs || result.NodesAdded >= githubMaxNodesTotal {
				logFn("info", "已达拉取/入库安全上限，提前结束")
				return true
			}
			urlKey := strings.ToLower(strings.TrimSpace(cand.URL))
			if urlKey == "" {
				continue
			}
			if _, ok := seenFileURL[urlKey]; ok {
				continue
			}
			seenFileURL[urlKey] = struct{}{}

			filesScanned++
			logFn("info", fmt.Sprintf("[有效 %d/%d] 拉取 %s score=%d source=%s path=%s",
				validCount, targetValid, truncateStr(cand.URL, 90), cand.Score, cand.Source, cand.Path))
			reporter.ReportProgress(validCount, cand.Path, nil)

			content, ferr := fetchHTTPContent(ctx, client, "", cand.URL)
			if ferr != nil || len(content) == 0 {
				if owner, name, ref, filePath, ok := parseGitHubRawURL(cand.URL); ok {
					content, ferr = fetchGitHubContentAPI(ctx, client, token, owner, name, ref, filePath)
				}
			}
			if ferr != nil || len(content) == 0 {
				logFn("warn", fmt.Sprintf("拉取失败 %s: %v", truncateStr(cand.URL, 90), ferr))
				continue
			}
			fetchedSubs++

			if !contentLooksLikeSubscription(content) {
				batchCheck := parseContentToProxies(ctx, client, content)
				if len(batchCheck) == 0 {
					logFn("info", fmt.Sprintf("跳过非订阅内容: %s", truncateStr(cand.Path, 60)))
					continue
				}
			}

			batch := make([]protocol.Proxy, 0, 64)
			for _, p := range parseContentToProxies(ctx, client, content) {
				link := GenerateProxyLink(p)
				if link == "" {
					continue
				}
				if _, exists := seenLinks[link]; exists {
					continue
				}
				seenLinks[link] = struct{}{}
				batch = append(batch, p)
				if len(batch) >= githubMaxNodesPerSub {
					break
				}
			}
			if len(batch) == 0 {
				logFn("warn", fmt.Sprintf("未解析到节点: %s", truncateStr(cand.URL, 90)))
				continue
			}

			remaining := targetValid - validCount
			// 测速数量：按剩余目标放大采样（约 3 倍），至少测 githubMaxTestPerSub；
			// 不再使用单文件 120 硬顶——目标仍大时应尽量测完本文件解析到的节点。
			maxTest := remaining * 3
			if maxTest < githubMaxTestPerSub {
				maxTest = githubMaxTestPerSub
			}
			if n := len(batch); n > 0 && maxTest > n {
				maxTest = n
			}

			logFn("info", fmt.Sprintf("解析到 %d 个节点，开始测速入库（本文件测 %d/%d，剩余目标 %d）",
				len(batch), maxTest, len(batch), remaining))
			allProxies = append(allProxies, batch...)
			batchNodes := proxiesToGitHubNodes(cfg.ID, runID, batch)
			a, te, pa := testAndSaveGitHubProxies(ctx, batchNodes, logFn, maxTest)
			result.NodesAdded += a
			validCount += pa
			logFn("info", fmt.Sprintf("订阅测速：测试 %d 通过 %d 新入库 %d；累计有效 %d/%d",
				te, pa, a, validCount, targetValid))
			reporter.ReportProgress(validCount, cand.Path, map[string]any{
				"valid": validCount, "target": targetValid, "added": result.NodesAdded,
			})
			if validCount >= targetValid {
				logFn("info", fmt.Sprintf("已达目标有效入库数 %d，结束抓取", targetValid))
				return true
			}
			if result.NodesAdded >= githubMaxNodesTotal {
				logFn("info", "已达总入库安全上限，提前结束")
				return true
			}
		}
		return validCount >= targetValid
	}

	collectRepoCandidates := func(repo githubRepoRef) []githubFileCandidate {
		cands := make([]githubFileCandidate, 0, 16)
		seen := make(map[string]struct{})
		add := func(c githubFileCandidate) {
			if c.URL == "" || c.Score < 20 {
				return
			}
			key := strings.ToLower(strings.TrimSpace(c.URL))
			if _, ok := seen[key]; ok {
				return
			}
			seen[key] = struct{}{}
			cands = append(cands, c)
		}

		for _, c := range codeByRepo[strings.ToLower(repo.FullName)] {
			add(c)
		}

		fileCands, ferr := discoverRecentFeatureFiles(ctx, client, token, repo, logFn)
		if ferr != nil {
			logFn("warn", fmt.Sprintf("仓库 %s 最近文件扫描失败: %v", repo.FullName, ferr))
		} else {
			for _, c := range fileCands {
				add(c)
			}
		}

		readme, readmeErr := fetchGitHubReadme(ctx, client, token, repo)
		if readmeErr != nil {
			logFn("warn", fmt.Sprintf("读取 README 失败 %s: %v", repo.FullName, readmeErr))
		} else if len(readme) > 0 {
			for _, subURL := range extractSubscriptionURLs(string(readme)) {
				add(githubFileCandidate{
					URL:       subURL,
					Repo:      repo.FullName,
					Path:      "README#sub",
					Source:    "readme",
					Score:     scoreSubscriptionURL(subURL),
					UpdatedAt: repo.Recency,
				})
			}
			if score := scoreContentHints(string(readme)); score >= 40 {
				raw := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/README.md",
					repo.FullName, firstNonEmpty(repo.DefaultBranch, "main"))
				add(githubFileCandidate{
					URL:       raw,
					Repo:      repo.FullName,
					Path:      "README.md",
					Source:    "readme_inline",
					Score:     score,
					UpdatedAt: repo.Recency,
				})
			}
		}

		if len(fileCands) == 0 {
			branch := firstNonEmpty(repo.DefaultBranch, "main")
			for _, pth := range commonSubPaths {
				sc := scoreFilePath(pth)
				if sc < 40 {
					continue
				}
				raw := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", repo.FullName, branch, pth)
				add(githubFileCandidate{
					URL:       raw,
					Repo:      repo.FullName,
					Path:      pth,
					Source:    "common_path",
					Score:     sc,
					UpdatedAt: repo.Recency,
				})
			}
		}
		return cands
	}

	// 按更新时间顺序逐库扫描
	for i, repo := range repos {
		select {
		case <-ctx.Done():
			result.FilesScanned = filesScanned
			result.NodesFound = len(allProxies)
			if validCount > 0 {
				msg := fmt.Sprintf("已取消：已扫描仓库 %d，文件 %d，有效入库 %d/%d", reposScanned, filesScanned, validCount, targetValid)
				result.Message = msg
				logFn("warn", msg)
				return result, ctx.Err()
			}
			return nil, ctx.Err()
		default:
		}
		if validCount >= targetValid {
			break
		}

		reposScanned++
		logFn("info", fmt.Sprintf("[有效 %d/%d · 仓库 %d/%d] 扫描 %s（更新时间：%s）",
			validCount, targetValid, i+1, len(repos), repo.FullName,
			firstNonEmpty(repo.PushedAt, repo.UpdatedAt)))

		cands := collectRepoCandidates(repo)
		if len(cands) == 0 {
			logFn("info", fmt.Sprintf("仓库 %s 未发现可提取文件，跳过", repo.FullName))
			continue
		}
		if processCandidates(repo.FullName, cands) {
			break
		}
	}

	// 处理无明确仓库归属的代码搜索孤儿文件
	if validCount < targetValid && len(orphanCode) > 0 {
		select {
		case <-ctx.Done():
		default:
			logFn("info", fmt.Sprintf("处理未归属仓库的代码特征文件 %d 个", len(orphanCode)))
			_ = processCandidates("orphan", orphanCode)
		}
	}

	result.FilesScanned = filesScanned
	result.NodesFound = len(allProxies)

	if len(allProxies) == 0 && result.NodesAdded == 0 && validCount == 0 {
		msg := fmt.Sprintf("扫描了 %d 个仓库、%d 个文件，但未得到有效节点（请换更具体的关键字）", reposScanned, filesScanned)
		logFn("warn", msg)
		result.Message = msg
		reporter.ReportComplete(msg, map[string]any{"repos": reposScanned, "files": filesScanned, "nodes_found": 0, "nodes_added": 0})
		return result, nil
	}

	msg := fmt.Sprintf("完成：扫描仓库 %d，文件 %d，拉取 %d，解析节点 %d，有效 %d/%d（新入库 %d）",
		reposScanned, filesScanned, fetchedSubs, len(allProxies), validCount, targetValid, result.NodesAdded)
	logFn("info", msg)
	utils.Info("[GitHubCrawl] config[%s] %s", cfg.Name, msg)
	result.Message = msg
	result.NodesAdded = validCount
	reporter.ReportComplete(msg, map[string]any{
		"repos":       reposScanned,
		"files":       filesScanned,
		"subs":        fetchedSubs,
		"nodes_found": len(allProxies),
		"nodes_added": result.NodesAdded,
		"nodes_valid": validCount,
		"target":      targetValid,
	})
	return result, nil
}
func proxiesToGitHubNodes(configID, runID int, proxies []protocol.Proxy) []models.GitHubCrawlNode {
	nodes := make([]models.GitHubCrawlNode, 0, len(proxies))
	for _, p := range proxies {
		link := GenerateProxyLink(p)
		if link == "" {
			continue
		}
		host := strings.TrimSpace(p.Server)
		port := ""
		if !p.Port.IsZero() {
			port = fmt.Sprintf("%d", p.Port.Int())
		}
		addr := host
		if host != "" && port != "" {
			addr = host + ":" + port
		}
		nodes = append(nodes, models.GitHubCrawlNode{
			ConfigID:    configID,
			RunID:       runID,
			Link:        link,
			Name:        strings.TrimSpace(p.Name),
			Protocol:    strings.TrimSpace(p.Type),
			LinkAddress: addr,
			LinkHost:    host,
			LinkPort:    port,
		})
	}
	return nodes
}

func testAndSaveGitHubProxies(ctx context.Context, nodes []models.GitHubCrawlNode, logFn GitHubLogFn, maxTest int) (added, tested, passed int) {
	if len(nodes) == 0 {
		return 0, 0, 0
	}
	limit := maxTest
	if limit <= 0 {
		limit = githubMaxTestPerSub
	}
	if limit <= 0 {
		limit = 25
	}
	if len(nodes) > limit {
		nodes = nodes[:limit]
	}
	conc := githubTestConcurrency
	if conc <= 0 {
		conc = 5
	}
	sem := make(chan struct{}, conc)
	var mu sync.Mutex
	var wg sync.WaitGroup
	passedNodes := make([]models.GitHubCrawlNode, 0, len(nodes))

	for i := range nodes {
		select {
		case <-ctx.Done():
			goto DONE
		default:
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(n models.GitHubCrawlNode) {
			defer wg.Done()
			defer func() { <-sem }()
			mu.Lock()
			tested++
			mu.Unlock()

			delay, _, _, err := mihomo.MihomoDelayTest(n.Link, "", 5*time.Second, true, false, "", false, "")
			if err != nil || delay <= 0 {
				return
			}
			speed, latency, _, _, _, err := mihomo.MihomoSpeedTest(n.Link, "", 8*time.Second, false, "", false, "", "average", 100)
			if err != nil || speed <= 0 {
				return
			}
			if latency > 0 {
				delay = latency
			}
			n.DelayTime = delay
			n.DelayStatus = "success"
			n.Speed = speed
			n.SpeedStatus = "success"
			n.IsValid = true
			mu.Lock()
			passed++
			passedNodes = append(passedNodes, n)
			mu.Unlock()
		}(nodes[i])
	}
DONE:
	wg.Wait()
	if len(passedNodes) == 0 {
		if logFn != nil {
			logFn("warn", fmt.Sprintf("测速完成：测试 %d，通过 0，未入库", tested))
		}
		return 0, tested, 0
	}
	n, err := models.UpsertGitHubCrawlNodes(passedNodes)
	if err != nil {
		if logFn != nil {
			logFn("error", "保存测速通过节点失败: "+err.Error())
		}
		return 0, tested, passed
	}
	if logFn != nil {
		logFn("info", fmt.Sprintf("测速完成：测试 %d，通过 %d，入库 %d", tested, passed, n))
	}
	return n, tested, passed
}

func discoverRecentFeatureFiles(ctx context.Context, client *http.Client, token string, repo githubRepoRef, logFn GitHubLogFn) ([]githubFileCandidate, error) {
	owner := firstNonEmpty(repo.Owner, strings.Split(repo.FullName, "/")[0])
	name := firstNonEmpty(repo.Name, "")
	if name == "" && strings.Contains(repo.FullName, "/") {
		parts := strings.SplitN(repo.FullName, "/", 2)
		owner, name = parts[0], parts[1]
	}
	if owner == "" || name == "" {
		return nil, fmt.Errorf("invalid repo full name: %s", repo.FullName)
	}

	listURL := fmt.Sprintf("%s/repos/%s/%s/commits?per_page=%d", githubAPIBase, url.PathEscape(owner), url.PathEscape(name), githubCommitLookback)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, err
	}
	setGitHubHeaders(req, token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list commits HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 120))
	}
	var commits []githubCommitListItem
	if err := json.Unmarshal(body, &commits); err != nil {
		return nil, err
	}
	if len(commits) == 0 {
		return nil, nil
	}

	out := make([]githubFileCandidate, 0, githubMaxFilesPerRepo*2)
	seenPath := make(map[string]struct{})
	skippedNoFeature := 0
	skippedExcluded := 0

	for _, c := range commits {
		if len(out) >= githubMaxFilesPerRepo {
			break
		}
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		default:
		}
		detailURL := fmt.Sprintf("%s/repos/%s/%s/commits/%s", githubAPIBase, url.PathEscape(owner), url.PathEscape(name), c.SHA)
		dreq, err := http.NewRequestWithContext(ctx, http.MethodGet, detailURL, nil)
		if err != nil {
			continue
		}
		setGitHubHeaders(dreq, token)
		dresp, err := client.Do(dreq)
		if err != nil {
			continue
		}
		dbody, _ := io.ReadAll(io.LimitReader(dresp.Body, 2<<20))
		_ = dresp.Body.Close()
		if dresp.StatusCode != http.StatusOK {
			continue
		}
		var detail githubCommitDetail
		if err := json.Unmarshal(dbody, &detail); err != nil {
			continue
		}
		updatedAt := parseGitHubTime(firstNonEmpty(
			detail.Commit.Committer.Date,
			detail.Commit.Author.Date,
			c.Commit.Committer.Date,
			c.Commit.Author.Date,
		))
		for _, f := range detail.Files {
			if len(out) >= githubMaxFilesPerRepo {
				break
			}
			fp := strings.TrimSpace(f.Filename)
			if fp == "" || strings.EqualFold(f.Status, "removed") {
				continue
			}
			low := strings.ToLower(fp)
			if _, ok := seenPath[low]; ok {
				continue
			}
			if excludePathPattern.MatchString(fp) {
				skippedExcluded++
				continue
			}
			sc := scoreFilePath(fp)
			if sc < 30 {
				skippedNoFeature++
				continue
			}
			seenPath[low] = struct{}{}
			rawURL := strings.TrimSpace(f.RawURL)
			if rawURL == "" {
				rawURL = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, name, c.SHA, fp)
			}
			out = append(out, githubFileCandidate{
				URL:       rawURL,
				Repo:      repo.FullName,
				Path:      fp,
				SHA:       c.SHA,
				Source:    "commit",
				Score:     sc,
				UpdatedAt: updatedAt,
			})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if len(out) > githubMaxFilesPerRepo {
		out = out[:githubMaxFilesPerRepo]
	}

	if logFn != nil && (skippedNoFeature > 0 || skippedExcluded > 0 || len(out) > 0) {
		logFn("info", fmt.Sprintf("仓库 %s 文件过滤：排除无关 %d，无特征 %d，保留 %d（同库上限 %d）",
			repo.FullName, skippedExcluded, skippedNoFeature, len(out), githubMaxFilesPerRepo))
	}
	return out, nil
}

func searchGitHubFeatureFiles(ctx context.Context, client *http.Client, token string, keywords []string, maxFiles int, logFn GitHubLogFn) ([]githubFileCandidate, error) {
	if maxFiles <= 0 {
		maxFiles = githubDefaultMaxLinks
	}
	featureQueries := []string{
		"clash extension:yml",
		"clash extension:yaml",
		"proxies extension:yaml",
		"mihomo extension:yml",
		"subscription extension:yaml",
		"filename:clash.yaml",
		"filename:clash.yml",
		"subscribe extension:txt",
	}
	for _, kw := range keywords {
		k := strings.TrimSpace(kw)
		if k == "" {
			continue
		}
		if len(k) > 40 || strings.Count(k, " ") > 4 {
			continue
		}
		featureQueries = append(featureQueries, k+" extension:yml")
		featureQueries = append(featureQueries, k+" extension:yaml")
		if len(featureQueries) >= 14 {
			break
		}
	}

	out := make([]githubFileCandidate, 0, maxFiles)
	seen := make(map[string]struct{})
	var lastErr error
	perPage := 8
	if maxFiles < perPage {
		perPage = maxFiles
	}

	for _, q := range featureQueries {
		if len(out) >= maxFiles {
			break
		}
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		default:
		}
		apiURL := fmt.Sprintf("%s/search/code?q=%s&sort=indexed&order=desc&per_page=%d",
			githubAPIBase, url.QueryEscape(q), perPage)
		if logFn != nil {
			logFn("info", fmt.Sprintf("代码搜索特征文件: %s", q))
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			lastErr = err
			continue
		}
		setGitHubHeaders(req, token)
		req.Header.Set("Accept", "application/vnd.github.text-match+json")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if logFn != nil {
				logFn("warn", fmt.Sprintf("代码搜索失败「%s」: %v", q, err))
			}
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			return out, fmt.Errorf("GitHub API 认证失败 (HTTP 401)：Token 无效或已过期")
		}
		if resp.StatusCode == http.StatusForbidden {
			lastErr = fmt.Errorf("GitHub Code Search 限流/拒绝 (HTTP 403)")
			if logFn != nil {
				logFn("warn", lastErr.Error()+"，后续仅用仓库提交扫描")
			}
			break
		}
		if resp.StatusCode == http.StatusUnprocessableEntity {
			if logFn != nil {
				logFn("warn", fmt.Sprintf("代码搜索查询无效，已跳过: %s", q))
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("code search HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 120))
			if logFn != nil {
				logFn("warn", lastErr.Error())
			}
			continue
		}

		var result githubCodeSearchResponse
		if err := json.Unmarshal(body, &result); err != nil {
			lastErr = err
			continue
		}
		if logFn != nil {
			logFn("info", fmt.Sprintf("特征查询「%s」命中 %d 条（取最近索引）", q, result.TotalCount))
		}
		for _, it := range result.Items {
			if len(out) >= maxFiles {
				break
			}
			fp := strings.TrimSpace(it.Path)
			if fp == "" {
				fp = strings.TrimSpace(it.Name)
			}
			if excludePathPattern.MatchString(fp) {
				continue
			}
			sc := scoreFilePath(fp)
			if sc < 30 {
				continue
			}
			full := strings.TrimSpace(it.Repository.FullName)
			if full == "" || !strings.Contains(full, "/") {
				continue
			}
			parts := strings.SplitN(full, "/", 2)
			sha := strings.TrimSpace(it.SHA)
			branch := firstNonEmpty(it.Repository.DefaultBranch, "main")
			ref := firstNonEmpty(sha, branch)
			rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", parts[0], parts[1], ref, fp)
			if rawFromHTML, ok := rawURLFromGitHubBlob(it.HTMLURL); ok {
				rawURL = rawFromHTML
			}
			key := strings.ToLower(rawURL)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, githubFileCandidate{
				URL:       rawURL,
				Repo:      full,
				Path:      fp,
				SHA:       sha,
				Source:    "code_search",
				Score:     sc + 10,
				UpdatedAt: parseGitHubTime(firstNonEmpty(it.Repository.PushedAt, it.Repository.UpdatedAt)),
			})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if lastErr != nil && len(out) == 0 {
		return out, lastErr
	}
	return out, nil
}

func rawURLFromGitHubBlob(htmlURL string) (string, bool) {
	u, err := url.Parse(htmlURL)
	if err != nil {
		return "", false
	}
	if !strings.EqualFold(u.Host, "github.com") {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "blob" {
		return "", false
	}
	owner, repo, ref := parts[0], parts[1], parts[3]
	filePath := strings.Join(parts[4:], "/")
	if owner == "" || repo == "" || ref == "" || filePath == "" {
		return "", false
	}
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, ref, filePath), true
}

func scoreFilePath(filePath string) int {
	fp := strings.TrimSpace(filePath)
	if fp == "" {
		return 0
	}
	if excludePathPattern.MatchString(fp) {
		return -100
	}
	low := strings.ToLower(fp)
	base := path.Base(low)
	ext := strings.ToLower(path.Ext(strings.Split(low, "?")[0]))
	score := 0

	switch ext {
	case ".yml", ".yaml":
		score += 50
	case ".txt", ".list", ".conf", ".sub":
		score += 30
	case ".json":
		score += 10
	case ".md", ".html", ".htm":
		score -= 40
	case ".go", ".py", ".js", ".ts", ".css", ".png", ".jpg":
		return -100
	}

	if featurePathPattern.MatchString(low) {
		score += 40
	}
	for _, kw := range []struct {
		s string
		w int
	}{
		{"clash", 35},
		{"mihomo", 30},
		{"meta", 15},
		{"proxies", 30},
		{"proxy", 15},
		{"subscribe", 35},
		{"subscription", 35},
		{"sub", 10},
		{"nodes", 25},
		{"node", 10},
		{"hysteria", 15},
		{"vless", 10},
		{"vmess", 10},
		{"trojan", 10},
		{"airport", 15},
		{"free", 8},
		{"provider", 12},
	} {
		if strings.Contains(base, kw.s) || strings.Contains(low, "/"+kw.s) {
			score += kw.w
		}
	}
	if base == "news" || base == "todo" || base == "readme" {
		return -100
	}
	return score
}

func scoreSubscriptionURL(raw string) int {
	m := strings.TrimSpace(raw)
	if m == "" {
		return 0
	}
	lower := strings.ToLower(m)
	if subExcludePattern.MatchString(m) {
		return -100
	}
	score := 10
	if clashURLPattern.MatchString(m) {
		score += 80
	}
	if weakSubPattern.MatchString(m) {
		score += 30
	}
	if nonClashPattern.MatchString(m) && !clashURLPattern.MatchString(m) {
		score -= 50
	}
	if strings.Contains(lower, "raw.githubusercontent.com") || strings.Contains(lower, "gist.githubusercontent.com") {
		score += 40
		if _, _, _, fp, ok := parseGitHubRawURL(m); ok {
			score += scoreFilePath(fp) / 2
		} else {
			ext := strings.ToLower(path.Ext(strings.Split(lower, "?")[0]))
			switch ext {
			case ".yml", ".yaml", ".txt", ".list", ".conf":
				score += 20
			case ".md", ".png", ".jpg":
				score -= 100
			}
		}
	}
	if featurePathPattern.MatchString(lower) {
		score += 20
	}
	return score
}

func scoreContentHints(s string) int {
	low := strings.ToLower(s)
	score := 0
	if strings.Contains(low, "proxies:") {
		score += 50
	}
	if strings.Contains(low, "proxy-providers:") || strings.Contains(low, "proxy-groups:") {
		score += 30
	}
	if proxyLinkPattern.MatchString(s) {
		score += 40
	}
	if strings.Contains(low, "subscribe") || strings.Contains(low, "subscription") {
		score += 20
	}
	if strings.Contains(low, "clash") || strings.Contains(low, "mihomo") {
		score += 15
	}
	return score
}

func contentLooksLikeSubscription(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	sample := data
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	s := string(sample)
	low := strings.ToLower(s)
	if strings.Contains(low, "proxies:") || strings.Contains(low, "proxy-providers:") {
		return true
	}
	if proxyLinkPattern.MatchString(s) {
		return true
	}
	if looksLikeBase64(strings.TrimSpace(s)) {
		return true
	}
	if strings.Contains(low, "vmess://") || strings.Contains(low, "vless://") ||
		strings.Contains(low, "trojan://") || strings.Contains(low, "ss://") ||
		strings.Contains(low, "hysteria") {
		return true
	}
	if strings.Contains(low, "type:") && (strings.Contains(low, "server:") || strings.Contains(low, "port:")) {
		return true
	}
	return false
}

func parseGitHubTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02T15:04:05Z", s); err == nil {
		return t
	}
	return time.Time{}
}

func enhanceSearchKeywords(keywords []string) []string {
	out := make([]string, 0, len(keywords)+len(defaultGitHubSearchQueries))
	seen := make(map[string]struct{})
	genericOnly := true
	for _, kw := range keywords {
		k := strings.TrimSpace(kw)
		if k == "" {
			continue
		}
		low := strings.ToLower(k)
		if _, ok := seen[low]; ok {
			continue
		}
		seen[low] = struct{}{}
		out = append(out, k)
		if strings.Contains(low, "yaml") || strings.Contains(low, "yml") ||
			strings.Contains(low, "subscription") || strings.Contains(low, "mihomo") ||
			strings.Contains(low, "proxypool") || strings.Contains(low, "hysteria") ||
			strings.Contains(low, "订阅") || strings.Contains(low, "免费节点") {
			genericOnly = false
		}
	}
	if genericOnly {
		for _, q := range defaultGitHubSearchQueries {
			low := strings.ToLower(q)
			if _, ok := seen[low]; ok {
				continue
			}
			seen[low] = struct{}{}
			out = append(out, q)
		}
	}
	return out
}

func splitSearchKeywords(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	replacer := strings.NewReplacer("\r\n", "\n", "\r", "\n", "，", ",", "；", "\n", ";", "\n")
	raw = replacer.Replace(raw)
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == ','
	})
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{})
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len(p) >= 2 && ((p[0] == '"' && p[len(p)-1] == '"') || (p[0] == '\'' && p[len(p)-1] == '\'')) {
			p = strings.TrimSpace(p[1 : len(p)-1])
		}
		low := strings.ToLower(p)
		if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
			continue
		}
		if _, ok := seen[low]; ok {
			continue
		}
		seen[low] = struct{}{}
		if len(p) > 120 {
			p = p[:120]
		}
		out = append(out, p)
	}
	return out
}

func searchGitHubRepos(ctx context.Context, client *http.Client, token string, keywords []string, maxRepos int, logFn GitHubLogFn) ([]githubRepoRef, error) {
	if maxRepos <= 0 {
		maxRepos = githubMaxRepos
	}
	repos := make([]githubRepoRef, 0, maxRepos)
	seen := make(map[string]struct{})
	var lastErr error

	perPage := githubSearchPageSize
	if maxRepos < perPage {
		perPage = maxRepos
	}

	for _, kw := range keywords {
		if len(repos) >= maxRepos {
			break
		}
		query := strings.TrimSpace(kw)
		if query == "" {
			continue
		}
		// 仓库搜索：简单自然语言，按最近更新排序；避免 Code Search 复杂 OR/协议前缀 422
		apiURL := fmt.Sprintf("%s/search/repositories?q=%s&sort=updated&order=desc&per_page=%d",
			githubAPIBase, url.QueryEscape(query), perPage)
		if logFn != nil {
			logFn("info", fmt.Sprintf("搜索仓库关键字: %s", kw))
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			lastErr = err
			continue
		}
		setGitHubHeaders(req, token)

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if logFn != nil {
				logFn("warn", fmt.Sprintf("搜索失败「%s」: %v", kw, err))
			}
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("GitHub API 认证失败 (HTTP 401)：Token 无效或已过期")
		}
		if resp.StatusCode == http.StatusForbidden {
			bodyHint := strings.TrimSpace(string(body))
			if len(bodyHint) > 180 {
				bodyHint = bodyHint[:180] + "..."
			}
			lastErr = fmt.Errorf("GitHub API 拒绝访问 (HTTP 403)：%s", bodyHint)
			if logFn != nil {
				logFn("warn", lastErr.Error())
			}
			break
		}
		if resp.StatusCode == http.StatusUnprocessableEntity {
			lastErr = fmt.Errorf("GitHub 搜索查询无法解析 (HTTP 422)：请简化关键字")
			if logFn != nil {
				logFn("warn", fmt.Sprintf("关键字「%s」查询无效，已跳过", kw))
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("GitHub API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			if logFn != nil {
				logFn("warn", lastErr.Error())
			}
			continue
		}

		var result githubRepoSearchResponse
		if err := json.Unmarshal(body, &result); err != nil {
			lastErr = err
			continue
		}
		if logFn != nil {
			logFn("info", fmt.Sprintf("关键字「%s」命中仓库 %d 个（取最近更新）", kw, result.TotalCount))
		}
		for _, item := range result.Items {
			full := strings.TrimSpace(item.FullName)
			if full == "" && item.Owner.Login != "" && item.Name != "" {
				full = item.Owner.Login + "/" + item.Name
			}
			if full == "" || !strings.Contains(full, "/") {
				continue
			}
			key := strings.ToLower(full)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			owner, name := item.Owner.Login, item.Name
			if owner == "" || name == "" {
				parts := strings.SplitN(full, "/", 2)
				if len(parts) == 2 {
					owner, name = parts[0], parts[1]
				}
			}
			recency := parseGitHubTime(firstNonEmpty(item.PushedAt, item.UpdatedAt))

			repos = append(repos, githubRepoRef{
				FullName:      full,
				Owner:         owner,
				Name:          name,
				HTMLURL:       item.HTMLURL,
				Description:   item.Description,
				DefaultBranch: firstNonEmpty(item.DefaultBranch, "main"),
				UpdatedAt:     item.UpdatedAt,
				PushedAt:      item.PushedAt,
				Recency:       recency,
			})
			if len(repos) >= maxRepos {
				break
			}
		}
	}

	if len(repos) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, nil
	}

	// 多关键词合并后按更新时间降序（最新在前）
	sort.SliceStable(repos, func(i, j int) bool {
		return repos[i].Recency.After(repos[j].Recency)
	})
	return repos, nil
}

func setGitHubHeaders(req *http.Request, token string) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "SublinkPro-GitHubCrawler")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
}

func fetchGitHubReadme(ctx context.Context, client *http.Client, token string, repo githubRepoRef) ([]byte, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/readme", githubAPIBase, repo.FullName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	setGitHubHeaders(req, token)
	req.Header.Set("Accept", "application/vnd.github.raw")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK {
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, githubMaxREADMEBytes))
		_ = resp.Body.Close()
		return data, readErr
	}
	_ = resp.Body.Close()

	branch := firstNonEmpty(repo.DefaultBranch, "main")
	names := []string{"README.md", "readme.md", "README.MD", "README", "Readme.md"}
	var lastFetchErr error
	for _, name := range names {
		raw := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", repo.FullName, branch, name)
		data, ferr := fetchHTTPContent(ctx, client, "", raw)
		if ferr == nil && len(data) > 0 {
			return data, nil
		}
		lastFetchErr = ferr
		if branch != "master" {
			raw2 := fmt.Sprintf("https://raw.githubusercontent.com/%s/master/%s", repo.FullName, name)
			data, ferr = fetchHTTPContent(ctx, client, "", raw2)
			if ferr == nil && len(data) > 0 {
				return data, nil
			}
			lastFetchErr = ferr
		}
	}
	if lastFetchErr != nil {
		return nil, lastFetchErr
	}
	return nil, fmt.Errorf("README not found")
}

func fetchGitHubContentAPI(ctx context.Context, client *http.Client, token, owner, repo, ref, filePath string) ([]byte, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s", githubAPIBase, owner, repo, strings.TrimPrefix(filePath, "/"))
	if ref != "" {
		apiURL += "?ref=" + url.QueryEscape(ref)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	setGitHubHeaders(req, token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, githubMaxContentBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var content githubContentResponse
	if err := json.Unmarshal(body, &content); err != nil {
		return body, nil
	}
	if strings.EqualFold(content.Encoding, "base64") && content.Content != "" {
		raw := strings.ReplaceAll(content.Content, "\n", "")
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			decoded, err = base64.URLEncoding.DecodeString(raw)
			if err != nil {
				return nil, err
			}
		}
		return decoded, nil
	}
	if content.Download != "" {
		return fetchHTTPContent(ctx, client, token, content.Download)
	}
	if content.Content != "" {
		return []byte(content.Content), nil
	}
	return nil, fmt.Errorf("empty content")
}

func fetchHTTPContent(ctx context.Context, client *http.Client, token, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "SublinkPro-GitHubCrawler")
	if token != "" && strings.Contains(rawURL, "api.github.com") {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, githubMaxContentBytes))
}

func parseContentToProxies(ctx context.Context, client *http.Client, data []byte) []protocol.Proxy {
	if len(data) == 0 {
		return nil
	}
	config, _, _ := parseClashConfigData(ctx, client, "", data, "", nil)
	if len(config.Proxies) > 0 {
		return config.Proxies
	}
	trimmed := strings.TrimSpace(string(data))
	if looksLikeBase64(trimmed) {
		compact := strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, trimmed)
		if decoded, err := base64.StdEncoding.DecodeString(compact); err == nil {
			config, _, _ = parseClashConfigData(ctx, client, "", decoded, "", nil)
			if len(config.Proxies) > 0 {
				return config.Proxies
			}
			data = decoded
		} else if decoded, err := base64.RawStdEncoding.DecodeString(compact); err == nil {
			config, _, _ = parseClashConfigData(ctx, client, "", decoded, "", nil)
			if len(config.Proxies) > 0 {
				return config.Proxies
			}
			data = decoded
		}
	}
	matches := proxyLinkPattern.FindAllString(string(data), -1)
	proxies := make([]protocol.Proxy, 0, len(matches))
	seen := make(map[string]struct{})
	for _, m := range matches {
		m = strings.TrimRight(m, ").,;]")
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		proxy, err := protocol.LinkToProxy(protocol.Urls{Url: m}, protocol.OutputConfig{})
		if err == nil {
			proxies = append(proxies, proxy)
		}
	}
	return proxies
}

func looksLikeBase64(s string) bool {
	if len(s) < 32 || len(s) > 2<<20 {
		return false
	}
	letters := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '+', c == '/', c == '=', c == '-', c == '_':
			letters++
		case c == '\n', c == '\r', c == ' ', c == '\t':
			continue
		default:
			return false
		}
	}
	return letters > len(s)*8/10
}

func extractSubscriptionURLs(content string) []string {
	matches := httpURLPattern.FindAllString(content, -1)
	type scored struct {
		score int
		url   string
	}
	items := make([]scored, 0)
	seen := make(map[string]struct{})

	for _, m := range matches {
		m = strings.TrimRight(m, ").,;]'\"`")
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		lower := strings.ToLower(m)
		if _, ok := seen[lower]; ok {
			continue
		}
		if subExcludePattern.MatchString(m) {
			continue
		}
		if !subHintPattern.MatchString(m) &&
			!strings.Contains(lower, "raw.githubusercontent.com") &&
			!strings.Contains(lower, "gist.githubusercontent.com") {
			continue
		}
		if strings.Contains(lower, "github.com") &&
			!strings.Contains(lower, "raw.githubusercontent.com") &&
			!strings.Contains(lower, "/raw/") &&
			!strings.Contains(lower, "gist.githubusercontent.com") {
			continue
		}
		score := 10
		if clashURLPattern.MatchString(m) {
			score += 80
		}
		if weakSubPattern.MatchString(m) {
			score += 30
		}
		if nonClashPattern.MatchString(m) && !clashURLPattern.MatchString(m) {
			score -= 50
		}
		if strings.Contains(lower, "raw.githubusercontent.com") || strings.Contains(lower, "gist.githubusercontent.com") {
			score += 40
			ext := strings.ToLower(path.Ext(strings.Split(lower, "?")[0]))
			switch ext {
			case ".yml", ".yaml", ".txt", ".list", ".conf":
				score += 20
			case ".md", ".png", ".jpg":
				score -= 100
			}
		}
		if score < 20 {
			continue
		}
		seen[lower] = struct{}{}
		items = append(items, scored{score: score, url: m})
	}

	hasStrong := false
	for _, it := range items {
		if it.score >= 80 {
			hasStrong = true
			break
		}
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].score > items[i].score {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if hasStrong && it.score < 40 {
			continue
		}
		out = append(out, it.url)
		if len(out) >= githubMaxSubURLs {
			break
		}
	}
	return out
}

func parseGitHubRawURL(rawURL string) (owner, repo, ref, filePath string, ok bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", "", false
	}
	host := strings.ToLower(u.Host)
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case host == "raw.githubusercontent.com":
		if len(parts) < 4 {
			return "", "", "", "", false
		}
		return parts[0], parts[1], parts[2], strings.Join(parts[3:], "/"), true
	case host == "github.com" && len(parts) >= 5 && parts[2] == "raw":
		return parts[0], parts[1], parts[3], strings.Join(parts[4:], "/"), true
	default:
		return "", "", "", "", false
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
