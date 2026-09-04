[English](system-update.md) | 简体中文

# 应用升级与成品库

SublinkPro 内置基于本地编译成品库的自升级系统：按当前运行平台精确下载发布产物，先以 test 模式试运行验证，再原子替换二进制并重启服务，同时支持一键回退到成品库中的任意历史版本。

## 功能概览

- **成品库**：下载与被替换下来的历史二进制统一存放在 `<db_path>/updater/artifacts/`，由 `versions.json`（JSON 版本账本）索引，记录版本、平台、大小、sha256、状态与时间戳。
- **升级源**（页面可配置）：
  - **JSON 清单**（推荐）：一个 URL 描述多个版本，每个版本携带多平台文件条目（`os`/`arch`/`url`/可选 `sha256`）。
  - **GitHub Releases**：直接枚举仓库全部历史发布与资产，按文件名自动匹配平台。
  - **单文件模板地址**：通过 `{os}`、`{arch}`、`{ext}`、`{version}` 占位符定位文件。
- **手动上传升级**：页面上传成品文件，自动执行与远程升级完全一致的 test→替换→重启 流水线。
- **平台识别**：应用以自身 `GOOS/GOARCH`（windows/linux/darwin × amd64/arm64 等）匹配清单，仅对可安装版本提供操作。
- **test 模式**：安装前用 `--version` 试运行候选成品，要求 20 秒内以退出码 0 结束且输出非空；不通过则丢弃产物、不做任何变更。
- **安全替换**：当前程序改名为 `.old` → 新文件写入原路径 → 重启进程（Unix 原地 `exec`；Windows 分离延迟启动器 + 优雅停机）。替换失败自动回滚改名。

## 清单格式

```json
{
  "latest": "v1.6.0",
  "versions": [
    {
      "version": "v1.6.0",
      "notes": "更新说明（可选）",
      "files": [
        { "os": "windows", "arch": "amd64", "url": "https://example.com/sublinkpro-v1.6.0-windows-amd64.exe", "sha256": "<hex, 可选>" },
        { "os": "linux",   "arch": "amd64", "url": "https://example.com/sublinkpro-v1.6.0-linux-amd64.tar.gz" },
        { "os": "linux",   "arch": "arm64", "url": "https://example.com/sublinkpro-v1.6.0-linux-arm64.zip" }
      ]
    }
  ]
}
```

文件可以是裸二进制，也可以是包含二进制的 `.zip` / `.tar.gz` 归档；提供 `sha256` 时下载后强制校验。

> [!TIP]
> 发布流水线（`.github/workflows/build-release.yml`）会在每次打 tag 发布时自动生成本清单——含每个文件的 sha256——并作为 `versions.json` 资产随 Release 发布。页面里填写的稳定地址为：
> `https://github.com/ZeroDeng01/sublinkPro/releases/latest/download/versions.json`
>
> 清单只列最新版本；更早的版本通过本地成品库回退（每次升级都会自动快照当时运行的二进制）。

## 使用方法

打开 **系统 → 更新日志（/system/updates）**：

1. **升级源配置**：选择源类型并填写地址，可选开启 mihomo 代理下载，设置成品保留数量后保存。
   - **JSON 版本清单**：`versions.json` 地址描述多版本多平台成品（推荐）。
   - **GitHub Releases**：填写 `owner/repo`（私有库/限流可配 Token）。应用直接调 GitHub API 枚举仓库全部历史发布，按文件名自动匹配运行平台资产（`-linux-amd64`、`-windows-x86.exe`、`darwin-arm64`、`linux_x86_64`、`win64` 等），无需 versions.json。
   - **单文件模板地址**：`{os} {arch} {ext} {version}` 占位符定位；无法列出历史版本。
2. **远程版本库**：点击「检查版本」列出全部已发布版本；没有当前平台文件的版本会标注不可安装。选择任意版本升级，或直接「立即升级到最新版」。
3. **本地成品库**：展示所有成品及状态（使用中 / 历史备份 / 存档），↩ 回退、🗑 删除旧成品（使用中版本不允许删除）。
4. **上传升级**：点击「上传升级」可直接上传本地编译成品（二进制 / zip / tar.gz，≤512MB）。上传后全自动执行流水线——test 模式试运行 → 快照当前版 → 原子替换 → 重启——零手动干预；上传的成品验证失败绝不会影响当前运行版本。可选填版本号覆盖试运行实测值。
5. 进度通过任务中心（`system_update` 类型任务）+ SSE 推送；页面每 3 秒轮询升级状态。

首次成功升级时，系统会把当前运行的二进制快照入库作为备份，因此永远存在至少一个回退点。

## 回退

对任意非 active 成品点击回退，将执行与升级完全相同的流水线——完整性校验、test 模式验证、原子替换、重启——只是方向相反：恢复选中的历史版本，被换下的当前版本转为 backup 状态。

## 落盘结构

```
<db_path>/updater/
├── config.json    # 升级源配置
├── versions.json  # 成品账本（schemaVersion/current/artifacts[]）
├── state.json     # 最近一次操作结果（重启后仍可查看）
├── artifacts/     # 成品文件：{版本}-{os}-{arch}{扩展名}
└── staging/       # 下载/解包临时目录
```

## 说明与限制

- 演示模式下所有写操作被禁止（保存配置、升级、回退、删除）。
- Unix 重启使用 `syscall.Exec`，监听 socket 随 exec 自动关闭，新二进制绑定同端口；Docker 内（二进制为 PID 1）同样适用，无需重启容器。
- Windows 通过分离的延迟启动器在旧进程优雅释放端口约 2 秒后拉起新二进制；若运行在不派生子进程的服务管理器下，请保留手动重启路径。
- 版本比较遵循 semver-lite 规则：忽略 `v` 前缀、数字段比较、预发布段（`-rc.1`）小于正式版。
- API 接口见 [skill-sublinkpro/reference/api.md](../../skill-sublinkpro/reference/api.md) 的 `/api/v1/updater/*` 部分。
