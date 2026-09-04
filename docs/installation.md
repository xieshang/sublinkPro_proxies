English | [简体中文](installation.zh-CN.md)

# Installation Guide

This document explains how to install, update, and uninstall SublinkPro.

---

## 📦 Run with Docker Compose, recommended

> [!TIP]
> **Docker Compose is recommended** because it makes configuration, upgrades, and maintenance easier.

> [!IMPORTANT]
> `db/`, `template/`, and `logs/` are runtime persistence directories. Keep them during upgrades and migrations.

Create `docker-compose.yml`:

```yaml
services:
  sublinkpro:
    # image: zerodeng/sublink-pro:dev # Development version, for trying new features
    image: zerodeng/sublink-pro # Stable version
    container_name: sublinkpro
    ports:
      - "8000:8000"
    volumes:
      - "./db:/app/db"
      - "./template:/app/template"
      - "./logs:/app/logs"
    restart: unless-stopped
```

Optional Sub-Store sidecar for expanded subscription output formats:

```yaml
services:
  sublinkpro:
    image: zerodeng/sublink-pro
    container_name: sublinkpro
    ports:
      - "8000:8000"
    volumes:
      - "./db:/app/db"
      - "./template:/app/template"
      - "./logs:/app/logs"
    restart: unless-stopped

  substore:
    image: xream/sub-store
    container_name: substore
    environment:
      - SUB_STORE_BACKEND_API_PORT=3000
      - SUB_STORE_BODY_JSON_LIMIT=10mb
    restart: unless-stopped
```

Keep the Sub-Store service inside the Compose network and do not publish its port unless you protect it separately. After both containers start, sign in and open **Application Settings -> Sub-Store** to enable the sidecar, set the base URL such as `http://substore:3000`, choose allowed output targets, and test the connection. Sub-Store integration is managed from that page, not through environment variables.

To expose the service through Cloudflare Tunnel, start the instance first, then open **Application Settings -> Cloudflare Tunnel**, enter the token, and start it. When auto connect is enabled, the Tunnel connects when the service starts. See [Cloudflare Tunnel remote access](features/cloudflare-tunnel.md) for the full flow.

The official Docker image includes `cloudflared`. Non Docker deployments need `cloudflared` installed first according to Cloudflare's official documentation.

Start the service:

```bash
docker-compose up -d
```

---

## 🐳 Run with Docker

<details>
<summary><b>Stable version</b></summary>

```bash
### GitHub 爬虫配置

支持独立 GitHub 节点抓取配置：

#### 新增字段
- `autoPromote`：抓取完成后自动加入总节点列表
- `maxCrawlLinks`：目标有效入库节点数
- `searchKeywords`：多行或逗号分隔，推荐使用 `clash free subscription` / `mihomo free nodes yaml`

#### 使用示例 (docker-compose.yml)
```yaml
services:
  sublinkpro:
    image: zerodeng/sublink-pro
    container_name: sublinkpro
    ports:
      - "8000:8000"
    environment:
      - SUBLINK_DB_PATH=/app/db
      - SUBLINK_LOG_PATH=/app/logs
      - SUBLINK_GITHUB_CRAWL_CONFIGS=1
    volumes:
      - ./db:/app/db
      - ./logs:/app/logs
    restart: unless-stopped
```

#### 启动抓取
1. 访问管理界面新建配置
2. 设置 `maxCrawlLinks`（建议从 10 开始测试）
3. 开启 `autoPromote`（可选）
4. 点击「开始」启动抓取
5. 停止时自动刷新配置列表

#### 注意事项
- 建议配置 GitHub Personal Access Token
- 同一仓库最多提取 3 个文件
- 抓取成功后可手动或自动加入总节点列表
- 支持代理拉取

有关详细配置请查看：
- `docs/features/github-crawl.md` (即将添加)
- `skill-sublinkpro/reference/api.md`


docker run --name sublinkpro -p 8000:8000 \
  -v $PWD/db:/app/db \
  -v $PWD/template:/app/template \
  -v $PWD/logs:/app/logs \
  -d zerodeng/sublink-pro
```

</details>

<details>
<summary><b>Development version, for trying new features</b></summary>

```bash
docker run --name sublinkpro -p 8000:8000 \
  -v $PWD/db:/app/db \
  -v $PWD/template:/app/template \
  -v $PWD/logs:/app/logs \
  -d zerodeng/sublink-pro:dev
```

</details>

---

## 📝 One Line Install or Update Script

```bash
sh -c "$(wget -qO- https://raw.githubusercontent.com/ZeroDeng01/sublinkPro/refs/heads/main/install.sh)"
```

> [!NOTE]
> The install script supports:
> - **Fresh install**: completes all setup automatically on first install
> - **Update**: detects an existing install and updates the program while keeping data
> - **Reinstall**: lets you choose whether to keep existing data
> - **Restore install**: detects old data and lets you restore it
> - **Architectures**: Linux x64, Linux ARM64, Linux ARMv7 32-bit, and Linux x86 32-bit

---

## 🗑️ One Line Uninstall Script

```bash
sh -c "$(wget -qO- https://raw.githubusercontent.com/ZeroDeng01/sublinkPro/refs/heads/main/uninstall.sh)"
```

> [!NOTE]
> The uninstall script asks whether to keep the data directories, including db, logs, and template. Keeping them allows later reinstalls to restore data.

---

## 🔄 Project Updates

### 📝 Update with the one line script

If you installed with the one line script, run the install script again to update:

```bash
sh -c "$(wget -qO- https://raw.githubusercontent.com/ZeroDeng01/sublinkPro/refs/heads/main/install.sh)"
```

The script detects the installed version and provides these options:

- **Update program**: keep all data and update program files only
- **Reinstall**: choose whether to keep data

### 📦 Manual Docker Compose update

```bash
# Enter the directory containing docker-compose.yml
cd /path/to/your/sublinkpro

# Pull the latest image
docker-compose pull

# Recreate and start the container
docker-compose up -d

# Optional: clean old images
docker image prune -f
```

### 🐳 Manual Docker update

```bash
# Stop and remove the old container
docker stop sublinkpro
docker rm sublinkpro

# Pull the latest image
docker pull zerodeng/sublink-pro

# Start the container again with the same parameters used during installation
docker run --name sublinkpro -p 8000:8000 \
  -v $PWD/db:/app/db \
  -v $PWD/template:/app/template \
  -v $PWD/logs:/app/logs \
  -d zerodeng/sublink-pro

# Optional: clean old images
docker image prune -f
```

---

## 🤖 Automatic Updates with Watchtower

Watchtower automatically updates Docker containers. It is useful if you want the project to stay current.

### Option 1: Run Watchtower separately

```bash
docker run -d \
  --name watchtower \
  -v /var/run/docker.sock:/var/run/docker.sock \
  containrrr/watchtower \
  --cleanup \
  --interval 86400 \
  sublinkpro
```

> [!NOTE]
> - `--cleanup`: remove old images after updates
> - `--interval 86400`: check for updates every 24 hours, in seconds
> - The final `sublinkpro` is the container name to monitor. If omitted, all containers are monitored.

### Option 2: Add Watchtower to Docker Compose

Add the Watchtower service to your `docker-compose.yml`:

```yaml
services:
  sublinkpro:
    image: zerodeng/sublink-pro
    container_name: sublinkpro
    ports:
      - "8000:8000"
    volumes:
      - "./db:/app/db"
      - "./template:/app/template"
      - "./logs:/app/logs"
    restart: unless-stopped

  watchtower:
    image: containrrr/watchtower
    container_name: watchtower
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    environment:
      - TZ=Asia/Shanghai
      - WATCHTOWER_CLEANUP=true
      - WATCHTOWER_POLL_INTERVAL=86400
    restart: unless-stopped
    command: sublinkpro  # Only monitor the sublinkpro container
```

> [!TIP]
> **Advanced Watchtower configuration**:
> - Set `WATCHTOWER_NOTIFICATIONS` to configure update notifications, including email, Slack, Gotify, and others
> - See the [official Watchtower documentation](https://containrrr.dev/watchtower/) for more settings

---

## 🚀 Built-in Application Upgrade (alternative to image updates)

SublinkPro also ships a self-upgrade system: open **System → System Updates**, point it at a JSON version manifest, and it downloads the artifact for the running platform, verifies it with a test-mode trial run, swaps the binary in place, and restarts — with one-click rollback to previously stored versions.

For Docker deployments:

- The artifact library lives under `./db/updater/` (persisted by the standard `./db:/app/db` volume), so rollback points survive container recreation.
- Inside the container the restart is an in-place `exec`; no special `restart:` policy or extra privileges are required. Expect a few seconds of downtime per upgrade.
- Each GitHub release publishes a `versions.json` manifest asset. The stable manifest URL is:
  `https://github.com/ZeroDeng01/sublinkPro/releases/latest/download/versions.json`
- ⚠️ Choose ONE primary update channel: `docker compose pull` reverts an in-app upgraded binary back to the image version. See [system-update feature guide](features/system-update.md) for details.
