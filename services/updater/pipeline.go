package updater

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sublink/utils"
)

// Reporter 升级进度上报回调（api 层桥接到任务中心/SSE）
type Reporter func(percent int, step, message string)

// noopReporter 空上报
func noopReporter(int, string, string) {}

// StartUpgrade 启动一次升级流水线（异步执行；同一时间仅允许一个升级/回退任务）。
// version 为空表示安装清单中的 latest（模板模式忽略该参数）。
func (m *Manager) StartUpgrade(version string, report Reporter) error {
	if report == nil {
		report = noopReporter
	}
	m.mutex.Lock()
	if err := m.acquire(); err != nil {
		m.mutex.Unlock()
		return err
	}
	m.mutex.Unlock()

	go func() {
		defer m.release()
		m.runUpgrade(version, report)
	}()
	return nil
}

// StartRollback 启动回退流水线：将指定成品重新验证后切换为运行版本并重启。
func (m *Manager) StartRollback(artifactID string, report Reporter) error {
	if report == nil {
		report = noopReporter
	}
	m.mutex.Lock()
	if err := m.acquire(); err != nil {
		m.mutex.Unlock()
		return err
	}
	m.mutex.Unlock()

	go func() {
		defer m.release()
		m.runRollback(artifactID, report)
	}()
	return nil
}

// StartUploadUpgrade 启动手动上传升级（异步执行；与远程升级共用 busy 锁）。
// 候选文件须已由 API 层落盘到 staging 目录并保留原始扩展名；
// 流程与远程升级完全一致：test 试运行 → 快照 → 原子替换 → 重启，
// 版本号优先取 claimedVersion，其次试运行实测输出。
func (m *Manager) StartUploadUpgrade(stagingPath string, report Reporter) error {
	if report == nil {
		report = noopReporter
	}
	if strings.TrimSpace(stagingPath) == "" {
		return fmt.Errorf("上传文件路径为空")
	}
	m.mutex.Lock()
	if err := m.acquire(); err != nil {
		m.mutex.Unlock()
		return err
	}
	m.mutex.Unlock()

	go func() {
		defer m.release()
		m.runUpload(stagingPath, report)
	}()
	return nil
}

// runUpload 手动上传升级流程：解包 → test → 入库 → 替换 → 重启
func (m *Manager) runUpload(stagingPath string, report Reporter) {
	action := "upload"
	state := &OpState{Action: action, Status: "running", StartedAt: time.Now()}
	_ = m.saveState(state)

	cleanupPaths := []string{stagingPath}
	defer func() {
		for _, p := range cleanupPaths {
			_ = os.Remove(p)
		}
	}()

	fail := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		utils.Error("[updater] 上传升级失败: %s", msg)
		state.Status = "failed"
		state.Message = msg
		state.FinishedAt = time.Now()
		_ = m.saveState(state)
		report(100, "failed", msg)
	}

	platform := DetectPlatform()
	report(3, "prepare", fmt.Sprintf("已接收上传文件，目标平台 %s/%s", platform.OS, platform.Arch))

	stamp := time.Now().Format("20060102150405")
	sourceExt := urlExtension(stagingPath)
	candidate := filepath.Join(m.stagingDir(), "candidate-"+stamp+platform.Ext)
	cleanupPaths = append(cleanupPaths, candidate)

	if err := m.ensureDirs(); err != nil {
		fail("%v", err)
		return
	}

	report(70, "extract", "成品提取完成")
	if xerr := extractBinary(stagingPath, candidate, sourceExt); xerr != nil {
		fail("成品提取失败: %v", xerr)
		return
	}
	if !m.installExtracted(candidate, "", "手动上传成品", "", stamp, report, state, fail) {
		return
	}
	state.Action = action // installExtracted 成功后 state.Version 已写入
	_ = m.saveState(state)
}

// runUpgrade 完整升级流程：
// 配置解析 → 拉取目标 → 下载 → 校验 → 解包 → test 试运行 → 入库 → 备份当前版 → 原子替换 → 重启
func (m *Manager) runUpgrade(targetVersion string, report Reporter) {
	action := "upgrade"
	state := &OpState{Action: action, Version: targetVersion, Status: "running", StartedAt: time.Now()}
	_ = m.saveState(state)

	cleanupPaths := []string{}
	defer func() {
		for _, p := range cleanupPaths {
			_ = os.Remove(p)
		}
	}()

	fail := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		utils.Error("[updater] 升级失败: %s", msg)
		state.Status = "failed"
		state.Message = msg
		state.FinishedAt = time.Now()
		_ = m.saveState(state)
		report(100, "failed", msg)
	}

	cfg, err := m.LoadConfig()
	if err != nil {
		fail("读取升级配置失败: %v", err)
		return
	}
	if err := cfg.Validate(); err != nil {
		fail("升级配置无效: %v", err)
		return
	}

	platform := DetectPlatform()
	report(3, "prepare", fmt.Sprintf("运行环境 %s/%s，开始准备下载", platform.OS, platform.Arch))

	var (
		downloadURL string
		expectSHA   string
		claimedVer  string
		notes       string
		sourceExt   string // 从 URL 推断的原始文件扩展名（用于识别归档）
	)
	switch cfg.SourceType {
	case SourceManifest:
		report(6, "manifest", "拉取远程版本清单…")
		manifest, ferr := m.FetchManifest(cfg)
		if ferr != nil {
			fail("%v", ferr)
			return
		}
		rv := (*RemoteVersion)(nil)
		if strings.TrimSpace(targetVersion) == "" || verKey(targetVersion) == verKey(manifest.Latest) {
			rv = FindRemoteVersion(manifest, manifest.Latest)
			targetVersion = manifest.Latest
		} else {
			rv = FindRemoteVersion(manifest, targetVersion)
		}
		if rv == nil {
			fail("清单中不存在版本 %s", targetVersion)
			return
		}
		rf, serr := PickRemoteFile(rv, platform)
		if serr != nil {
			fail("%v", serr)
			return
		}
		downloadURL, expectSHA, claimedVer, notes = rf.URL, rf.SHA256, rv.Version, rv.Notes
		report(10, "manifest", fmt.Sprintf("已定位版本 %s 的 %s/%s 成品", rv.Version, platform.OS, platform.Arch))
	case SourceGitHub:
		report(6, "manifest", "拉取 GitHub Releases…")
		manifest, ferr := m.FetchGitHubReleases(cfg)
		if ferr != nil {
			fail("%v", ferr)
			return
		}
		rv := (*RemoteVersion)(nil)
		if strings.TrimSpace(targetVersion) == "" || verKey(targetVersion) == verKey(manifest.Latest) {
			rv = FindRemoteVersion(manifest, manifest.Latest)
			targetVersion = manifest.Latest
		} else {
			rv = FindRemoteVersion(manifest, targetVersion)
		}
		if rv == nil {
			fail("Releases 中不存在版本 %s", targetVersion)
			return
		}
		rf, serr := PickRemoteFile(rv, platform)
		if serr != nil {
			fail("%v", serr)
			return
		}
		downloadURL, expectSHA, claimedVer, notes = rf.URL, rf.SHA256, rv.Version, rv.Notes
		report(10, "manifest", fmt.Sprintf("已定位版本 %s 的 %s/%s 资产", rv.Version, platform.OS, platform.Arch))
	case SourceTemplate:
		downloadURL = RenderTemplate(cfg.TemplateURL, platform.OS, platform.Arch, platform.Ext, targetVersion)
		claimedVer = targetVersion
	default:
		fail("不支持的升级源类型: %s", cfg.SourceType)
		return
	}
	sourceExt = urlExtension(downloadURL)

	if err := m.ensureDirs(); err != nil {
		fail("%v", err)
		return
	}

	// ---- 下载 ----
	stamp := time.Now().Format("20060102150405")
	downloaded := filepath.Join(m.stagingDir(), "download-"+stamp+sourceExt)
	candidate := filepath.Join(m.stagingDir(), "candidate-"+stamp+platform.Ext)
	cleanupPaths = append(cleanupPaths, downloaded, candidate)

	report(16, "download", "开始下载成品…")
	client, _, cerr := utils.CreateProxyHTTPClient(cfg.UseProxy, "", 30*time.Minute)
	if cerr != nil {
		fail("创建下载客户端失败: %v", cerr)
		return
	}
	n, derr := m.doDownload(client, downloadURL, downloaded)
	if derr != nil {
		fail("下载失败: %v", derr)
		return
	}
	report(55, "download", fmt.Sprintf("下载完成（%s）", humanSize(n)))

	// ---- sha256 校验 ----
	sum, verr := verifySHA256(downloaded, expectSHA)
	if verr != nil {
		fail("%v", verr)
		return
	}
	report(62, "verify", fmt.Sprintf("sha256 校验通过: %s", shortHash(sum)))

	// ---- 解包提取二进制 ----
	if xerr := extractBinary(downloaded, candidate, sourceExt); xerr != nil {
		fail("%v", xerr)
		return
	}
	if !m.installExtracted(candidate, claimedVer, notes, downloadURL, stamp, report, state, fail) {
		return
	}
}

// installExtracted 公共安装段（远程升级与手动上传共用）：
// test 试运行 → 版本解析 → 入库 → 快照当前版 → 原子替换 → 重启。
// 候选文件必须已落盘 staging 且完成解包；返回 false 表示失败
// （fail 已写 state 并上报进度）。
func (m *Manager) installExtracted(
	candidatePath, claimedVer, notes, sourceURL, stamp string,
	report Reporter,
	state *OpState,
	fail func(string, ...any),
) bool {
	platform := DetectPlatform()

	// ---- test 模式试运行（失败产物不入库，随 defer 清理）----
	report(76, "test", "test 模式：试运行 --version 验证…")
	tr := TestBinary(candidatePath)
	if !tr.OK {
		fail("test 模式未通过：%s%s", tr.Err, tailOutput(tr.Output))
		return false
	}
	observed := tr.ObservedVer
	if claimedVer == "" || claimedVer == "unknown" {
		claimedVer = observed
	}
	if claimedVer == "" {
		claimedVer = "manual-" + stamp
	}
	warnNote := ""
	if observed != "" && verKey(observed) != verKey(claimedVer) &&
		!strings.Contains(verKey(observed), verKey(claimedVer)) &&
		!strings.Contains(verKey(claimedVer), verKey(observed)) {
		warnNote = fmt.Sprintf("实测版本 %s 与标注 %s 不一致", observed, claimedVer)
		utils.Warn("[updater] %s", warnNote)
	}
	report(84, "test", fmt.Sprintf("test 通过（版本 %s）", claimedVer))

	// ---- 安装：入库 + 替换 ----
	finalID := ArtifactID(claimedVer, platform.OS, platform.Arch)
	finalName := ArtifactFileName(claimedVer, platform.OS, platform.Arch, platform.Ext)
	finalPath := filepath.Join(m.artifactDir(), finalName)

	ledger, lerr := m.LoadLedger()
	if lerr != nil {
		fail("%v", lerr)
		return false
	}

	// 快照当前运行的二进制入库（首次升级时保证随时可回退）
	if serr := m.snapshotCurrent(ledger); serr != nil {
		utils.Warn("[updater] 快照当前版本失败（不影响本次升级）: %v", serr)
	}

	if cerr := copyFile(candidatePath, finalPath, 0o755); cerr != nil {
		fail("成品入库失败: %v", cerr)
		return false
	}
	finalSum, _ := verifySHA256(finalPath, "")
	now := time.Now()

	// 覆盖同 ID 旧记录或新增 active 记录
	entry := m.FindArtifact(ledger, finalID)
	if entry == nil {
		entry = &Artifact{ID: finalID}
		ledger.Artifacts = append(ledger.Artifacts, entry)
	}
	entry.Version = claimedVer
	entry.ObservedVer = observed
	entry.OS = platform.OS
	entry.Arch = platform.Arch
	entry.FileName = finalName
	entry.Size = fileSize(finalPath)
	entry.SHA256 = finalSum
	entry.Status = ArtifactStatusActive
	entry.SourceURL = sourceURL
	entry.Note = joinNonEmpty(notes, warnNote)
	entry.TestedAt = now
	entry.ActivatedAt = now

	// 其它 active 记录全部转为 backup
	for _, a := range ledger.Artifacts {
		if a.ID != finalID && a.Status == ArtifactStatusActive {
			a.Status = ArtifactStatusBackup
		}
	}
	ledger.Current = claimedVer
	if serr := m.SaveLedger(ledger); serr != nil {
		fail("保存版本账本失败: %v", serr)
		return false
	}

	// ---- 磁盘原子替换 ----
	report(90, "install", "替换运行中的程序文件…")
	// 必须在 rename 之前解析可执行路径：Linux 的 /proc/self/exe 会随 rename 变化，
	// 替换后再取会拿到 .old 陈旧路径导致 exec 重启失败
	exePath, epErr := executablePath()
	if epErr != nil {
		fail("获取当前可执行文件路径失败: %v", epErr)
		return false
	}
	if serr := swapBinary(candidatePath, exePath); serr != nil {
		// 账本回退到替换前状态由下次操作自愈；这里必须报告失败
		fail("替换程序文件失败: %v", serr)
		return false
	}

	state.Status = "success"
	state.Version = claimedVer
	state.Message = fmt.Sprintf("已更新到 %s，正在重启", claimedVer)
	state.FinishedAt = time.Now()
	_ = m.saveState(state)
	report(96, "install", state.Message)

	// ---- 重启 ----
	report(100, "restart", "服务即将重启以加载新版本…")
	go func() {
		time.Sleep(500 * time.Millisecond) // 给进度广播留出窗口
		if rerr := RestartSelf(exePath); rerr != nil {
			utils.Error("[updater] 重启失败: %v（新版本文件已就位，请手动重启进程）", rerr)
		}
	}()
	return true
}

// runRollback 回退流程：校验成品 → test 再验证 → 切换账本 → 原子替换 → 重启
func (m *Manager) runRollback(artifactID string, report Reporter) {
	action := "rollback"
	state := &OpState{Action: action, Version: artifactID, Status: "running", StartedAt: time.Now()}
	_ = m.saveState(state)

	fail := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		utils.Error("[updater] 回退失败: %s", msg)
		state.Status = "failed"
		state.Message = msg
		state.FinishedAt = time.Now()
		_ = m.saveState(state)
		report(100, "failed", msg)
	}

	platform := DetectPlatform()
	ledger, err := m.LoadLedger()
	if err != nil {
		fail("%v", err)
		return
	}
	target := m.FindArtifact(ledger, artifactID)
	if target == nil {
		fail("成品 %s 不存在", artifactID)
		return
	}
	if target.Status == ArtifactStatusActive {
		fail("成品 %s 就是当前运行版本，无需回退", artifactID)
		return
	}
	if target.OS != platform.OS || target.Arch != platform.Arch {
		fail("成品平台 %s/%s 与运行环境 %s/%s 不匹配", target.OS, target.Arch, platform.OS, platform.Arch)
		return
	}
	srcPath := filepath.Join(m.artifactDir(), target.FileName) // #nosec G304 -- 受管目录内拼接
	if _, err := os.Stat(srcPath); err != nil {
		fail("成品文件缺失: %v", err)
		return
	}

	report(10, "verify", "核对成品完整性…")
	if _, err := verifySHA256(srcPath, target.SHA256); err != nil {
		fail("成品校验失败: %v", err)
		return
	}

	report(25, "test", "test 模式：试运行验证历史成品…")
	tr := TestBinary(srcPath)
	if !tr.OK {
		fail("回退成品 test 未通过：%s%s", tr.Err, tailOutput(tr.Output))
		return
	}
	report(45, "test", fmt.Sprintf("test 通过（输出版本 %s）", tr.ObservedVer))

	now := time.Now()
	for _, a := range ledger.Artifacts {
		if a.Status == ArtifactStatusActive && a.ID != target.ID {
			a.Status = ArtifactStatusBackup
		}
	}
	target.Status = ArtifactStatusActive
	target.ActivatedAt = now
	target.TestedAt = now
	if tr.ObservedVer != "" {
		target.ObservedVer = tr.ObservedVer
	}
	ledger.Current = target.Version
	if err := m.SaveLedger(ledger); err != nil {
		fail("保存版本账本失败: %v", err)
		return
	}

	report(65, "install", "回退替换运行中的程序文件…")
	// 同 runUpgrade：rename 前解析路径，避免 /proc/self/exe 指向 .old
	exePath, epErr := executablePath()
	if epErr != nil {
		fail("获取当前可执行文件路径失败: %v", epErr)
		return
	}
	if err := swapBinary(srcPath, exePath); err != nil {
		fail("替换程序文件失败: %v", err)
		return
	}

	state.Status = "success"
	state.Version = target.Version
	state.Message = fmt.Sprintf("已回退到 %s，正在重启", target.Version)
	state.FinishedAt = time.Now()
	_ = m.saveState(state)
	report(85, "install", state.Message)

	report(100, "restart", "服务即将重启以恢复历史版本…")
	go func() {
		time.Sleep(500 * time.Millisecond)
		if rerr := RestartSelf(exePath); rerr != nil {
			utils.Error("[updater] 重启失败: %v（历史版本文件已就位，请手动重启进程）", rerr)
		}
	}()
}

// snapshotCurrent 把当前运行的二进制快照进成品库（status=backup），确保随时可回退
func (m *Manager) snapshotCurrent(ledger *Ledger) error {
	cur := m.AppVersion()
	platform := DetectPlatform()
	id := ArtifactID(cur, platform.OS, platform.Arch)
	existing := m.FindArtifact(ledger, id)
	if existing != nil && existing.FileName != "" {
		if f := filepath.Join(m.artifactDir(), existing.FileName); fileSize(f) > 0 { // #nosec G304 -- 受管目录内拼接
			return nil // 库里已有该版本的可用副本
		}
	}
	exePath, err := executablePath()
	if err != nil {
		return err
	}
	fileName := ArtifactFileName(cur, platform.OS, platform.Arch, platform.Ext)
	dest := filepath.Join(m.artifactDir(), fileName)
	if err := copyFile(exePath, dest, 0o755); err != nil {
		return err
	}
	sum, _ := verifySHA256(dest, "")
	if existing == nil {
		existing = &Artifact{ID: id}
		ledger.Artifacts = append(ledger.Artifacts, existing)
	}
	existing.Version = cur
	existing.OS = platform.OS
	existing.Arch = platform.Arch
	existing.FileName = fileName
	existing.Size = fileSize(dest)
	existing.SHA256 = sum
	existing.TestedAt = time.Now() // 正在运行的版本本身就是被验证过的
	existing.CreatedAt = time.Now()
	if existing.Status == "" || existing.Status == ArtifactStatusArchived {
		existing.Status = ArtifactStatusBackup
	}
	if ledger.Current == "" {
		ledger.Current = cur
		existing.Status = ArtifactStatusActive
	}
	utils.Info("[updater] 当前版本快照入库: %s", id)
	return m.SaveLedger(ledger)
}

// swapBinary 原子替换运行中的可执行文件：
// rename 当前 exe → .old（运行中的进程仍持有映像句柄，Windows 允许改名），
// 复制候选成品到原路径；失败则回滚改名。残留 .old 在下次替换前自动清理。
// exePath 必须由调用方在 rename 之前解析好传入——rename 之后
// /proc/self/exe 会指向 .old，此时再取就是陈旧路径。
func swapBinary(candidatePath, exePath string) error {
	if exePath == "" {
		return fmt.Errorf("可执行文件路径为空")
	}
	oldPath := exePath + ".old"
	_ = os.Remove(oldPath) // 清理上次可能残留的旧版
	if err := os.Rename(exePath, oldPath); err != nil {
		return fmt.Errorf("备份当前程序失败: %w", err)
	}
	if err := copyFile(candidatePath, exePath, 0o755); err != nil {
		if rbErr := os.Rename(oldPath, exePath); rbErr != nil {
			return fmt.Errorf("写入新程序失败且回滚失败（旧版保留在 %s）: %w", oldPath, err)
		}
		return fmt.Errorf("写入新程序失败（已回滚）: %w", err)
	}
	_ = os.Remove(oldPath) // Unix 立即删除；Windows 映像占用时留待下次清理
	return nil
}

// ---- 小工具 ----

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src) // #nosec G304 -- 受管路径
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm) // #nosec G304 -- 受管路径
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func urlExtension(rawURL string) string {
	clean := rawURL
	if idx := strings.IndexAny(clean, "?#"); idx >= 0 {
		clean = clean[:idx]
	}
	lower := strings.ToLower(clean)
	// 复合后缀必须先于单段后缀判断
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return ".tar.gz"
	case strings.HasSuffix(lower, ".tar.bz2"):
		return ".tar.bz2"
	case strings.HasSuffix(lower, ".zip"):
		return ".zip"
	case strings.HasSuffix(lower, ".tgz"):
		return ".tgz"
	default:
		return ""
	}
}

func shortHash(sum string) string {
	if len(sum) > 12 {
		return sum[:12] + "…"
	}
	return sum
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func joinNonEmpty(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, strings.TrimSpace(p))
		}
	}
	return strings.Join(kept, "；")
}

func tailOutput(output string) string {
	if strings.TrimSpace(output) == "" {
		return ""
	}
	const maxKeep = 300
	out := strings.TrimSpace(output)
	if len(out) > maxKeep {
		out = out[:maxKeep]
	}
	return "；输出: " + out
}
