package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sublink/utils"
)

// maxDownloadBytes 下载体积上限（512MB），防止误配地址拖爆磁盘
const maxDownloadBytes = 512 << 20

// downloadFile 流式下载远程文件到 destPath，返回实际字节数
func (m *Manager) downloadFile(url, destPath string, useProxy bool) (int64, error) {
	client, _, err := utils.CreateProxyHTTPClient(useProxy, "", 30*time.Minute)
	if err != nil {
		return 0, fmt.Errorf("创建下载客户端失败: %w", err)
	}
	return m.doDownload(client, url, destPath)
}

// doDownload 实际执行下载（独立出来便于测试）
func (m *Manager) doDownload(client *http.Client, url, destPath string) (int64, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "SublinkPro-Updater")
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("下载请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}
	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755) // #nosec G304 -- 受管 staging 路径
	if err != nil {
		return 0, fmt.Errorf("创建临时文件失败: %w", err)
	}
	n, err := io.Copy(out, io.LimitReader(resp.Body, maxDownloadBytes+1))
	closeErr := out.Close()
	if err != nil {
		_ = os.Remove(destPath)
		return 0, fmt.Errorf("写入下载数据失败: %w", err)
	}
	if closeErr != nil {
		_ = os.Remove(destPath)
		return 0, fmt.Errorf("关闭临时文件失败: %w", closeErr)
	}
	if n > maxDownloadBytes {
		_ = os.Remove(destPath)
		return 0, fmt.Errorf("文件超过大小上限 %dMB", maxDownloadBytes>>20)
	}
	if n == 0 {
		_ = os.Remove(destPath)
		return 0, fmt.Errorf("下载内容为空")
	}
	return n, nil
}

// verifySHA256 校验文件 sha256；expect 为空时仅计算返回
func verifySHA256(path, expect string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- 受管路径
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if expect != "" && !strings.EqualFold(sum, strings.TrimSpace(expect)) {
		return sum, fmt.Errorf("sha256 校验失败：期望 %s，实际 %s", expect, sum)
	}
	return sum, nil
}

// extractBinary 从下载文件中提取可执行二进制到 destPath。
// 支持 zip / tar.gz / tar.bz2 归档（取归档内第一个可执行或匹配名的成员），裸二进制直接移动。
func extractBinary(downloaded, destPath, platformExt string) error {
	lower := strings.ToLower(downloaded)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractFromZip(downloaded, destPath)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractFromTarGz(downloaded, destPath)
	case strings.HasSuffix(lower, ".tar.bz2"), strings.HasSuffix(lower, ".tbz2"):
		return fmt.Errorf("暂不支持 tar.bz2 归档，请提供 zip / tar.gz 或裸二进制")
	default:
		// 裸二进制：直接改名到位（保留已验证的执行位）
		if err := os.Rename(downloaded, destPath); err != nil {
			return fmt.Errorf("移动成品文件失败: %w", err)
		}
		return nil
	}
}

// pickArchiveMember 从归档成员名列表里挑出主程序：
// 优先匹配含 sublink 的名字，其次任意无扩展名/与平台扩展名一致的成员。
func pickArchiveMember(names []string, platformExt string) string {
	var candidates []string
	for _, n := range names {
		base := filepath.Base(strings.ReplaceAll(n, "\\", "/"))
		if base == "" || strings.HasPrefix(base, ".") {
			continue
		}
		candidates = append(candidates, base)
	}
	for _, c := range candidates {
		if strings.Contains(strings.ToLower(c), "sublink") && (platformExt == "" || strings.HasSuffix(c, platformExt)) {
			return c
		}
	}
	for _, c := range candidates {
		if platformExt != "" && strings.HasSuffix(c, platformExt) {
			return c
		}
	}
	for _, c := range candidates {
		if filepath.Ext(c) == "" || platformExt == "" {
			return c
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

func extractFromZip(src, destPath string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("打开 zip 失败: %w", err)
	}
	defer func() { _ = r.Close() }()

	names := make([]string, 0, len(r.File))
	byBase := map[string]*zip.File{}
	for _, f := range r.File {
		names = append(names, f.Name)
		byBase[filepath.Base(f.Name)] = f
	}
	target := pickArchiveMember(names, DetectPlatform().Ext)
	if target == "" {
		return fmt.Errorf("zip 中未找到可提取的二进制")
	}
	file := byBase[target]
	rc, err := file.Open()
	if err != nil {
		return fmt.Errorf("读取 zip 成员失败: %w", err)
	}
	defer func() { _ = rc.Close() }()
	if err := writeExecutable(destPath, rc); err != nil {
		return err
	}
	utils.Info("[updater] 从 zip 提取成品: %s", target)
	return nil
}

func extractFromTarGz(src, destPath string) error {
	f, err := os.Open(src) // #nosec G304 -- 受管路径
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("打开 gzip 失败: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	var names []string
	members := map[string]*tar.Header{}
	readers := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		names = append(names, hdr.Name)
		members[filepath.Base(hdr.Name)] = hdr
		content, rerr := io.ReadAll(tr)
		if rerr != nil {
			return fmt.Errorf("读取 tar 成员失败: %w", rerr)
		}
		readers[filepath.Base(hdr.Name)] = content
	}
	target := pickArchiveMember(names, DetectPlatform().Ext)
	if target == "" {
		return fmt.Errorf("tar.gz 中未找到可提取的二进制")
	}
	if err := writeExecutable(destPath, strings.NewReader(string(readers[target]))); err != nil {
		return err
	}
	_ = members // 命中即用内容写出
	utils.Info("[updater] 从 tar.gz 提取成品: %s", target)
	return nil
}

// writeExecutable 将内容写为目标文件并赋予执行权限
func writeExecutable(destPath string, r io.Reader) error {
	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755) // #nosec G304 -- 受管路径
	if err != nil {
		return fmt.Errorf("写出成品失败: %w", err)
	}
	if _, err := io.Copy(out, r); err != nil {
		_ = out.Close()
		return fmt.Errorf("写出成品内容失败: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("关闭成品文件失败: %w", err)
	}
	return ensureExecBit(destPath)
}
