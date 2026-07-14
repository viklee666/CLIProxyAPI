# CLIProxyAPI 自构建单容器部署

本仓库只构建并启动 `cli-proxy-api:local`。该镜像在同一容器内集成：

- CLIProxyAPI 后端；
- CPA Manager Plus 管理页面和 Manager Server；
- 请求采集、SQLite 聚合、账号巡检及自动恢复能力。

Compose 不包含独立的 `cpa-manager-plus` 或反向代理服务，也不构建反向代理镜像。容器内 Manager Plus 只监听 `127.0.0.1:18317`，CLIProxyAPI 在 `8317` 端口按路径转发，因此外部反向代理只需连接 `cli-proxy-api:8317`。

## 1. 准备配置

```bash
cp .env.example .env
mkdir -p auths logs plugins data/cpa-manager-plus
```

编辑 `.env`。Docker Compose 会自动读取该文件，后续命令不需要再指定 `--env-file`：

- `CPA_MANAGEMENT_KEY` 必须与 `config.yaml` 的 `remote-management.secret-key` 一致；
- `CPA_DOCKER_NETWORK` 填写现有反向代理容器已加入的 Docker 网络名称，当前部署默认是 `shared_proxy`；
- 集成模式统一使用 `CPA_MANAGEMENT_KEY` 登录，首次启动或从旧 Manager 数据迁移时会自动同步内置管理凭据；
- 数据目录使用宿主机 bind mount，便于备份和迁移；
- `GOPROXY` 默认使用 `https://goproxy.cn,direct`，可按服务器网络调整。

Compose 将该网络声明为 `external`，不会创建、重建或修改现有反向代理容器及其配置。

建议在 `config.yaml` 使用：

```yaml
remote-management:
  allow-remote: true
  secret-key: "与 CPA_MANAGEMENT_KEY 相同"
  disable-auto-update-panel: true

usage-statistics-enabled: true
redis-usage-queue-retention-seconds: 1800

routing:
  strategy: "round-robin"
  session-affinity: false
```

`disable-auto-update-panel` 可防止 CLIProxyAPI 后台更新器把内置的 CPA Manager Plus 页面替换回原管理页。

## 2. 网络与端口

主服务只通过 Docker 网络声明：

```yaml
expose:
  - "8317"
```

`8317` 不再发布到宿主机。既有反向代理应通过共享 Docker 网络访问 `http://cli-proxy-api:8317`。OAuth 浏览器回调端口仍按原项目行为发布到宿主机，不经过反向代理。

请确保 `CPA_DOCKER_NETWORK` 指向已经存在的网络：

```bash
docker network inspect "$(grep '^CPA_DOCKER_NETWORK=' .env | cut -d= -f2-)"
```

## 3. 构建与启动

```bash
docker compose build --pull cli-proxy-api
docker compose up -d cli-proxy-api
docker compose ps
```

单命令重建：

```bash
docker compose up -d --build cli-proxy-api
```

启动过程只管理 `cli-proxy-api`，不会操作外部 `nginx-proxy`。

## 4. 路由结果

外部反向代理把 CPA 域名流量转发到 `cli-proxy-api:8317` 后：

- `/management.html`、`/manager-assets/*`、`/usage-service/*` 和 Manager Plus 专属聚合接口进入容器内 Manager Plus；
- 普通 `/v0/management/*` 进入 CLIProxyAPI 原管理 API；
- 其他模型/API 流量直接进入 CLIProxyAPI。

CLI 镜像使用 CPAMP 多文件生产构建：`management.html` 只负责启动应用，JS/CSS 通过 `/manager-assets/` 提供并按 hash 长期缓存，不再返回单个超大 HTML。

## 5. 从旧容器迁移

1. 停止旧 `cli-proxy-api` 和独立 `cpa-manager-plus`，保持服务器现有反向代理不变；
2. 将原 CLI 配置、凭证和日志目录分别指向 `CLI_PROXY_CONFIG_PATH`、`CLI_PROXY_AUTH_PATH`、`CLI_PROXY_LOG_PATH`；
3. 将原 CPA Manager Plus `/data` 内容复制到 `CPA_MANAGER_DATA_PATH`，保留 `usage.sqlite` 与 `data.key`；
4. 将 `CPA_DOCKER_NETWORK` 设置为外部反向代理当前使用的 Docker 网络；
5. 执行上面的构建启动命令。

## 6. 验证

```bash
docker compose config --quiet
docker compose ps
docker compose logs --tail=200 cli-proxy-api
docker inspect cli-proxy-api --format '{{json .NetworkSettings.Networks}}'
```

通过现有公开域名验证：

```bash
curl -fsS https://YOUR_CPA_DOMAIN/healthz
curl -fsSI https://YOUR_CPA_DOMAIN/management.html
```

凭证分页接口示例：

```bash
curl -H "Authorization: Bearer $CPA_MANAGEMENT_KEY" \
  'https://YOUR_CPA_DOMAIN/v0/management/auth-files?view=summary&page=1&page_size=50&provider=codex&sort=name'
```

响应包含 `total`、`page`、`page_size`、`total_pages`、`has_more` 和提供商 facets。

## 7. 上游更新

CLIProxyAPI 核心改动集中在：

- `internal/api/handlers/management/auth_files.go` 的一个兼容分流点；
- 新增 `auth_files_query.go`；
- `internal/api/modules/managerplus` 的可选本机反向代理桥；
- Docker/部署文件。

CPA Manager Plus 保持在 `extensions/cpa-manager-plus` 独立目录，基线提交记录见 `extensions/cpa-manager-plus/UPSTREAM.md`。更新 CLIProxyAPI 上游时不会与整个前端/Manager Server 树交叉冲突。
