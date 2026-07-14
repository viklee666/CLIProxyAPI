# CLIProxyAPI 自构建一体化部署

本仓库现在构建两个本地镜像：

- `cli-proxy-api:local`：CLIProxyAPI 后端，并在同一镜像、同一容器内集成 CPA Manager Plus 页面、请求采集、SQLite 聚合、账号巡检与自动恢复能力；
- `cpa-nginx-proxy:local`：统一反向代理配置。

Compose 中不再存在 `cpa-manager-plus` 服务。容器内的 Manager Plus 进程只监听 `127.0.0.1:18317`，由 CLIProxyAPI 在 `8317` 端口按路径转发，Nginx 只连接 `cli-proxy-api:8317`。

## 1. 准备配置

```bash
cp .env.stack.example .env.stack
mkdir -p auths logs plugins data/cpa-manager-plus
```

编辑 `.env.stack`：

- `CPA_MANAGEMENT_KEY` 必须与 `config.yaml` 的 `remote-management.secret-key` 一致；
- 集成模式统一使用 `CPA_MANAGEMENT_KEY` 登录；首次启动或从旧 Manager 数据迁移时会自动同步内置管理凭据；
- 填写 API/管理域名和证书绝对路径；
- 数据目录使用宿主机 bind mount，便于备份和迁移。
- `GOPROXY` 默认使用 `https://goproxy.cn,direct`，可按服务器网络改为其他 Go module proxy。

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

## 2. 构建与启动

```bash
docker compose --env-file .env.stack build --pull
docker compose --env-file .env.stack up -d
docker compose --env-file .env.stack ps
```

单命令重建：

```bash
docker compose --env-file .env.stack up -d --build
```

## 3. 路由结果

- `https://API_DOMAIN/management.html`、`/manager-assets/*`、`/usage-service/*` 和 Manager Plus 专属聚合接口 -> 容器内 Manager Plus；
- 普通 `https://API_DOMAIN/v0/management/*` -> CLIProxyAPI 原管理 API；
- `https://API_DOMAIN/*` -> CLIProxyAPI 模型/API 流量；
- `https://MANAGER_DOMAIN/*` -> 同一个 `cli-proxy-api:8317` 入口。

因此原 CLIProxyAPI 管理页在公开入口被完全替换，同时 API 流式请求仍直接进入 CLIProxyAPI。

Nginx 镜像和 CLI 镜像都使用 CPAMP 的多文件生产构建：`management.html` 只负责启动应用，JS/CSS 通过 `/manager-assets/` 提供并按 hash 长期缓存。直接访问 `cli-proxy-api:8317/management.html` 也不会再下载单个超大 HTML。

## 4. 从旧容器迁移

1. 停止旧 `cli-proxy-api`、`cpa-manager-plus`、`nginx-proxy`；新 Compose 启动后只会创建 `cli-proxy-api` 与 `nginx-proxy`；
2. 将原 CLI 配置、凭证和日志目录分别指向 `CLI_PROXY_CONFIG_PATH`、`CLI_PROXY_AUTH_PATH`、`CLI_PROXY_LOG_PATH`；
3. 将原 CPA Manager Plus `/data` 内容复制到 `CPA_MANAGER_DATA_PATH`，保留 `usage.sqlite` 与 `data.key`；
4. 将证书路径写入 `.env.stack`；
5. 执行上面的构建启动命令。

## 5. 验证

```bash
curl -fsS https://API_DOMAIN/healthz
curl -fsS https://MANAGER_DOMAIN/health
docker compose --env-file .env.stack logs --tail=200 cli-proxy-api nginx-proxy
```

凭证分页接口示例：

```bash
curl -H "Authorization: Bearer $CPA_MANAGEMENT_KEY" \
  'https://API_DOMAIN/v0/management/auth-files?view=summary&page=1&page_size=50&provider=codex&sort=name'
```

响应包含 `total`、`page`、`page_size`、`total_pages`、`has_more` 和提供商 facets。

## 6. 上游更新

CLIProxyAPI 核心改动集中在：

- `internal/api/handlers/management/auth_files.go` 的一个兼容分流点；
- 新增 `auth_files_query.go`；
- `internal/api/modules/managerplus` 的可选本机反向代理桥；
- Docker/部署文件。

CPA Manager Plus 保持在 `extensions/cpa-manager-plus` 独立目录，基线提交记录见 `extensions/cpa-manager-plus/UPSTREAM.md`。更新 CLIProxyAPI 上游时不会与整个前端/Manager Server 树交叉冲突。
