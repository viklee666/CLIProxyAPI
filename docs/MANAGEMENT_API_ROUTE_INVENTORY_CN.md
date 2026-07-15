# Management API 全量路由与性能处置清单

更新日期：2026-07-16。本文是当前共享工作树的路由快照；核心静态路由来自 `internal/api/server.go`，Manager Plus 分流来自 `extensions/cpa-manager-plus/apps/manager-server/internal/http/router/router.go` 及各 controller 的 method/path 分支。

关键证据入口：

- 核心静态注册：`internal/api/server.go:781` 起。
- Manager Plus mux 与反代分流：`extensions/cpa-manager-plus/apps/manager-server/internal/http/router/router.go:27,59` 起。
- 动态插件注册/分发：`internal/pluginhost/management.go:40,232` 起。
- 前端管理前缀：`extensions/cpa-manager-plus/apps/web/src/utils/constants.ts:17`、`utils/connection.ts:19`。
- Manager Plus 精确 method/path 来自 `internal/http/controller/{usage,modelprice,apikeyalias,accountaction,codexinspection,dashboard,monitoring,managerconfig,automation,quotacooldown,setup,system,health}/handler.go`。
- 每条路由的 `已优化/天然有界/仍有风险` 三态结论见 [MANAGEMENT_API_DISPOSITION_CN.md](MANAGEMENT_API_DISPOSITION_CN.md)。

## 运行时边界

- Docker 构建的真实后台源码是 `extensions/cpa-manager-plus/apps/web/src`，不是仓库里的占位 `management.html`。
- 浏览器访问 8317；Manager Plus 在 18317 处理自己的路由，其余 `/v0/management/*` 反代至 CLIProxyAPI 核心。
- 核心当前有 152 个静态 `method + path` 组合；另有插件运行时动态注册集合。
- Manager Plus 当前自有 35 个非 OPTIONS `method + path` 组合（含 health、setup、静态后台入口）。
- CORS middleware 对相应端点另外处理 `OPTIONS`，不作为业务接口重复计数。

## 接口族处置矩阵

| 接口族/匹配路径 | 主要调用者 | 数据源 | 原有边界 | 风险与最终处置 |
|---|---|---|---|---|
| `/config`、`/config.yaml`、普通开关 | 登录、配置页、Dashboard | 内存 `config.Config` + YAML | 原 `/config` 日常全量；YAML 原先无 body 上限 | 日常 bootstrap 改 `view=ui`：各集合最多 200 个轻量 identity，并返回 totals/truncated；无参数/full 兼容响应限 32 MiB。YAML GET/PUT 均限 16 MiB，GET 流式。 |
| `/api-keys` | System、模型探测、配置页 | YAML/内存数组 | 原全数组 GET/PUT，PATCH/DELETE 单项 | GET 已默认 100/最大 200 分页并支持 search；新增单记录 POST；System/Dashboard 只取首项，旧 PUT 仅作兼容批量入口且请求体/条目数有硬上限。 |
| `/*-api-key`、`/openai-compatibility` | AI Providers 页面 | YAML/内存 provider 数组 | 原 UI 每次 GET `/config` + PUT 整段 | 已增加记录级 POST/PATCH，按 auth-index/base-url 精确匹配；GET 默认 100/最大 200，前端自动续页；旧 PUT 兼容保留并限制 4 MiB/10,000 条。 |
| `/auth-files*`、`/model-definitions/:channel` | Auth Files、Quota、Client Groups、监控元数据 | auth manager + auth JSON 文件 | 查询参数存在，但每次仍遍历/排序全体；旧无参数返回重 detail | `detail/summary/snapshot/count` 均有界分页；2 秒候选 catalog 跨页复用并在写操作后失效，cursor 增量读取实时状态；复杂回退改轻量 summary；批量上传/状态/字段/ZIP 及单文件、总请求、目标数均有硬上限。 |
| `/client-access/*` | Client Groups、Client Keys | `client-access.sqlite` | page/page_size；原有 N+1 与逐条绑定 | 已消除 key relation N+1、集合式替换、服务端 query selector；支持全部、plan、provider、排除项。SQLite 自动迁移，无额外部署命令。 |
| `/logs*`、`/request-error-logs*`、`/request-log-by-id/*` | Logs、Dashboard | 文件系统 | `/logs` cursor 可用但 limit 无上限；错误日志无分页；ID 每次扫目录 | 当前树已设日志默认 1000/最大 10000；错误日志支持 page/page_size/search/count，目录 catalog 复用；Dashboard 仅取 count。文件下载仍按用户显式触发流式发送。 |
| `/plugins*`、`/plugin-store*` | 插件页、主导航 | plugin host、registry/release API、YAML | 原轮询全表；registry 重复下载；动态 handler body 无界 | plugins/store/routes 均默认 100、最大 200 分页，前端自动续页；store 先分页再 release enrichment；registry/release 使用 TTL 缓存与有界 worker；动态请求/响应 4/16 MiB。 |
| OAuth URL/status/callback/session、Vertex import | OAuth/导入页 | auth session、token store、文件 | 状态轮询；单次操作 | session 已有 TTL；callback 限 64 KiB 并限制字段长度，Vertex multipart/文件限 2/1 MiB。OAuth excluded/alias 按扁平条目分页，模型定义按 channel 分页/search。 |
| `/api-call` | provider 健康检查、Quota、模型探测 | 受管 HTTP client | 请求/响应由探测目标决定 | 属于用户明确禁止改动的 LLM/模型/配额探测链；本次保持原请求、响应与 token refresh 行为不变。 |
| `/api-key-usage` | provider 最近请求 UI | 内存 usage 统计 | 扫描全部 key/provider | 已改为 provider/search 筛选后的稳定扁平分页，默认 100、最大 200；仅为当前页构造 recent-request buckets，前端自动续页并兼容旧嵌套响应。 |
| `/usage-queue` | Manager Plus collector | 内存/Redis queue | destructive GET，count 无上限 | 当前树 count 最大 1000；Manager 默认 batch=100 不变。 |
| `/routing/adaptive/scores` | Adaptive Routing 页 | runtime score snapshot | 无分页 | 已增加 provider/model/search/eligible 服务端过滤与稳定分页，默认 100、最大 200；配置 GET/PUT 本身为小对象。 |
| Manager `/usage`、export/import | 兼容/迁移工具 | `usage.sqlite` | `/usage` 最多 50k；export 流式；import 64 MiB | legacy `/usage` 已改为默认 200、最大 500 的 page 或快照 keyset cursor；export/import 继续流式/分批并保留 64 MiB 导入边界。 |
| Manager `/monitoring/analytics`、account-history/header snapshots | Monitoring、Usage Analytics、Auth/Quota | `usage.sqlite`、rollup | include 投影和 event keyset 已有；多聚合无 limit，部分 raw rows 在 Go 聚合 | model/account/credential/api-key stats 已在 SQL 层独立分页并限制 200；account-history 最多 200 targets，header snapshots 强制 days<=365、limit<=2000。 |
| Manager `/dashboard/summary` | Dashboard | rollup + usage events | top/recent 参数 | 保持聚合响应，并对 top/recent 参数实施有界查询；Dashboard 不再额外下载完整配置、日志目录或原始 provider 集合做计数。 |
| Manager `/account-action-candidates*` | Account Actions | `usage.sqlite` | 原 limit<=500，无 page/total | `analytics_pagination` 子任务已增加 page/page_size/search/status/total，旧 limit 兼容。 |
| Manager `/codex-inspection/*` | Codex Inspection | `usage.sqlite` + auth files | runs 仅 limit；detail 返回全部 results/logs | runs 已分页（最大 100）；detail 的 results/logs 独立分页（各最大 200）并支持投影；run/action 响应不再回完整详情，动作 IDs 按 200 分块。 |
| Manager model-prices、aliases、quota cooldown | 配置/Quota | `usage.sqlite`/配置 | 原完整 map/list | model-prices 与 aliases 已由 SQLite 直接分页/search，前端自动续页；aliases 批量写最多 1000 且只回变更项；prices PUT 最多 10,000；sync models<=500、上游响应<=32 MiB，完成后分页重载。cooldown 已分页/过滤。 |
| 插件动态 Management API | 插件自定义页面 | plugin runtime | 路由运行时注册，静态无法枚举 | 已新增分页只读 `/v0/management/plugins/routes`，返回 plugin_id/method/path/version；动态请求/响应有硬上限，可供回归测试比对。 |

## Manager Plus 自有路由（逐 method/path）

| 方法 | 路径 | Controller/用途 | 当前数据边界 |
|---|---|---|---|
| `GET` | `/health` | health | 小响应 |
| `GET` | `/status` | system status | 聚合计数，小响应 |
| `GET` | `/usage-service/info` | feature/setup 探测 | 小响应 |
| `GET` | `/usage-service/config` | manager config | 配置对象 |
| `PUT` | `/usage-service/config` | manager config 更新 | JSON 4 MiB 总闸 |
| `GET` | `/usage-service/account-processing-policy` | automation status | 小响应 |
| `PATCH` | `/usage-service/account-processing-policy` | automation update | JSON 4 MiB 总闸 |
| `GET` | `/usage-service/quota-cooldowns` | active cooldowns | provider/auth/search/page，最大页 200 |
| `POST` | `/setup` | 首次配置 | JSON 4 MiB 总闸；一次性 |
| `GET` | `/management.html` | 后台入口 | 静态文件 |
| `GET` | `/manager-assets/*` | 后台 hash assets | immutable 静态资源 |
| `GET` | `/v0/management/usage` | legacy compatible usage | page/cursor，默认 200、最大 500 |
| `GET` | `/v0/management/usage/export` | NDJSON export | 流式、QueryLimit |
| `POST` | `/v0/management/usage/import` | usage import | 64 MiB、分批提交 |
| `GET` | `/v0/management/model-prices` | price list | SQLite page/page_size/search，最大页 200 |
| `PUT` | `/v0/management/model-prices` | replace prices | 4 MiB、最多 10,000 项 |
| `GET` | `/v0/management/model-prices/usage-summary` | model usage 聚合 | 聚合结果、QueryLimit |
| `POST` | `/v0/management/model-prices/sync` | price sync | models<=500、上游<=32 MiB、不回整库 |
| `GET` | `/v0/management/api-key-aliases` | alias list | SQLite page/page_size/search，最大页 200 |
| `PUT` | `/v0/management/api-key-aliases` | alias save | 4 MiB、最多 1000 项、只回变更项 |
| `DELETE` | `/v0/management/api-key-aliases/:hash` | alias delete | 单项 |
| `GET` | `/v0/management/account-action-candidates` | candidates page | 当前树 page/page_size<=200/search/status/total |
| `POST` | `/v0/management/account-action-candidates/:id/ignore` | action | 单项 |
| `POST` | `/v0/management/account-action-candidates/:id/resolve` | action | 单项 |
| `POST` | `/v0/management/account-action-candidates/:id/enable` | action | 单项 |
| `DELETE` | `/v0/management/account-action-candidates/:id/auth-file` | action | 单项 |
| `POST` | `/v0/management/codex-inspection/cooldown-disable` | cooldown action | 单项 JSON |
| `POST` | `/v0/management/codex-inspection/run` | start inspection | 单次任务；响应不携带完整详情 |
| `GET` | `/v0/management/codex-inspection/runs` | run list | page/page_size，最大页 100 |
| `GET` | `/v0/management/codex-inspection/runs/:id` | run detail | results/logs 独立分页，各最大页 200 |
| `POST` | `/v0/management/codex-inspection/runs/:id/actions` | bulk actions | 单请求最多 200 result IDs |
| `GET` | `/v0/management/dashboard/summary` | dashboard aggregate | top/recent 参数 |
| `POST` | `/v0/management/monitoring/analytics` | analytics include 投影 | events keyset；各实体 aggregate 独立分页<=200 |
| `POST` | `/v0/management/monitoring/account-history` | account history | JSON 4 MiB；最多 200 targets |
| `GET` | `/v0/management/monitoring/header-snapshots` | header snapshots | days<=365、limit<=2000 |

其余 `/v0/management/*` 由 Manager Plus `proxyHandler.Management` 转发给下方核心路由；插件资源路径另由 proxy resource 分支处理。

## 插件运行时动态路由

- 注册入口：`internal/pluginhost/management.go` 的 `RegisterManagementRoutes`。
- 分发入口：`ServeManagementHTTP`，键为 `METHOD + normalized full path`。
- 约束：必须位于 `/v0/management/...`，不能与核心静态路由冲突；含菜单的 legacy GET 会转为 `/v0/resource/plugins/...` 资源路由。
- 具体插件路由无法仅靠源码静态列出；现已通过分页只读 `GET /v0/management/plugins/routes` 暴露当前 runtime inventory，可在插件启停/安装后的回归中按 method/path/plugin_id/version 自动比对，保证动态集合不遗漏。

## 核心静态路由（逐 method/path，共 152）

| 方法 | 路径 | Handler |
|---|---|---|
| `GET` | `/v0/management/anthropic-auth-url` | `RequestAnthropicToken` |
| `GET` | `/v0/management/antigravity-auth-url` | `RequestAntigravityToken` |
| `POST` | `/v0/management/api-call` | `APICall` |
| `GET` | `/v0/management/api-key-usage` | `GetAPIKeyUsage` |
| `DELETE` | `/v0/management/api-keys` | `DeleteAPIKeys` |
| `GET` | `/v0/management/api-keys` | `GetAPIKeys` |
| `PATCH` | `/v0/management/api-keys` | `PatchAPIKeys` |
| `POST` | `/v0/management/api-keys` | `PostAPIKey` |
| `PUT` | `/v0/management/api-keys` | `PutAPIKeys` |
| `DELETE` | `/v0/management/auth-files` | `DeleteAuthFile` |
| `GET` | `/v0/management/auth-files` | `ListAuthFiles` |
| `POST` | `/v0/management/auth-files` | `UploadAuthFile` |
| `GET` | `/v0/management/auth-files/download` | `DownloadAuthFile` |
| `POST` | `/v0/management/auth-files/download` | `DownloadAuthFilesBatch` |
| `PATCH` | `/v0/management/auth-files/fields` | `PatchAuthFileFields` |
| `PATCH` | `/v0/management/auth-files/fields/batch` | `PatchAuthFileFieldsBatch` |
| `GET` | `/v0/management/auth-files/models` | `GetAuthFileModels` |
| `PATCH` | `/v0/management/auth-files/status` | `PatchAuthFileStatus` |
| `PATCH` | `/v0/management/auth-files/status/batch` | `PatchAuthFileStatusBatch` |
| `DELETE` | `/v0/management/claude-api-key` | `DeleteClaudeKey` |
| `GET` | `/v0/management/claude-api-key` | `GetClaudeKeys` |
| `PATCH` | `/v0/management/claude-api-key` | `PatchClaudeKey` |
| `POST` | `/v0/management/claude-api-key` | `PostClaudeKey` |
| `PUT` | `/v0/management/claude-api-key` | `PutClaudeKeys` |
| `GET` | `/v0/management/client-access/credential-bindings` | `ListClientAccessCredentialBindings` |
| `PUT` | `/v0/management/client-access/credential-bindings` | `ReplaceClientAccessCredentialBindings` |
| `POST` | `/v0/management/client-access/credential-bindings/bulk` | `BulkReplaceClientAccessCredentialBindings` |
| `GET` | `/v0/management/client-access/groups` | `ListClientAccessGroups` |
| `POST` | `/v0/management/client-access/groups` | `CreateClientAccessGroup` |
| `DELETE` | `/v0/management/client-access/groups/:id` | `DeleteClientAccessGroup` |
| `GET` | `/v0/management/client-access/groups/:id` | `GetClientAccessGroup` |
| `PATCH` | `/v0/management/client-access/groups/:id` | `UpdateClientAccessGroup` |
| `GET` | `/v0/management/client-access/keys` | `ListClientAccessKeys` |
| `POST` | `/v0/management/client-access/keys` | `CreateClientAccessKey` |
| `DELETE` | `/v0/management/client-access/keys/:id` | `DeleteClientAccessKey` |
| `GET` | `/v0/management/client-access/keys/:id` | `GetClientAccessKey` |
| `PATCH` | `/v0/management/client-access/keys/:id` | `UpdateClientAccessKey` |
| `DELETE` | `/v0/management/codex-api-key` | `DeleteCodexKey` |
| `GET` | `/v0/management/codex-api-key` | `GetCodexKeys` |
| `PATCH` | `/v0/management/codex-api-key` | `PatchCodexKey` |
| `POST` | `/v0/management/codex-api-key` | `PostCodexKey` |
| `PUT` | `/v0/management/codex-api-key` | `PutCodexKeys` |
| `GET` | `/v0/management/codex-auth-url` | `RequestCodexToken` |
| `GET` | `/v0/management/config` | `GetConfig` |
| `GET` | `/v0/management/config.yaml` | `GetConfigYAML` |
| `PUT` | `/v0/management/config.yaml` | `PutConfigYAML` |
| `GET` | `/v0/management/config/summary` | `GetConfigSummary` |
| `GET` | `/v0/management/debug` | `GetDebug` |
| `PATCH` | `/v0/management/debug` | `PutDebug` |
| `PUT` | `/v0/management/debug` | `PutDebug` |
| `GET` | `/v0/management/error-logs-max-files` | `GetErrorLogsMaxFiles` |
| `PATCH` | `/v0/management/error-logs-max-files` | `PutErrorLogsMaxFiles` |
| `PUT` | `/v0/management/error-logs-max-files` | `PutErrorLogsMaxFiles` |
| `GET` | `/v0/management/force-model-prefix` | `GetForceModelPrefix` |
| `PATCH` | `/v0/management/force-model-prefix` | `PutForceModelPrefix` |
| `PUT` | `/v0/management/force-model-prefix` | `PutForceModelPrefix` |
| `DELETE` | `/v0/management/gemini-api-key` | `DeleteGeminiKey` |
| `GET` | `/v0/management/gemini-api-key` | `GetGeminiKeys` |
| `PATCH` | `/v0/management/gemini-api-key` | `PatchGeminiKey` |
| `POST` | `/v0/management/gemini-api-key` | `PostGeminiKey` |
| `PUT` | `/v0/management/gemini-api-key` | `PutGeminiKeys` |
| `GET` | `/v0/management/get-auth-status` | `GetAuthStatus` |
| `DELETE` | `/v0/management/interactions-api-key` | `DeleteInteractionsKey` |
| `GET` | `/v0/management/interactions-api-key` | `GetInteractionsKeys` |
| `PATCH` | `/v0/management/interactions-api-key` | `PatchInteractionsKey` |
| `POST` | `/v0/management/interactions-api-key` | `PostInteractionsKey` |
| `PUT` | `/v0/management/interactions-api-key` | `PutInteractionsKeys` |
| `GET` | `/v0/management/kimi-auth-url` | `RequestKimiToken` |
| `GET` | `/v0/management/latest-version` | `GetLatestVersion` |
| `GET` | `/v0/management/logging-to-file` | `GetLoggingToFile` |
| `PATCH` | `/v0/management/logging-to-file` | `PutLoggingToFile` |
| `PUT` | `/v0/management/logging-to-file` | `PutLoggingToFile` |
| `DELETE` | `/v0/management/logs` | `DeleteLogs` |
| `GET` | `/v0/management/logs` | `GetLogs` |
| `GET` | `/v0/management/logs-max-total-size-mb` | `GetLogsMaxTotalSizeMB` |
| `PATCH` | `/v0/management/logs-max-total-size-mb` | `PutLogsMaxTotalSizeMB` |
| `PUT` | `/v0/management/logs-max-total-size-mb` | `PutLogsMaxTotalSizeMB` |
| `GET` | `/v0/management/max-retry-interval` | `GetMaxRetryInterval` |
| `PATCH` | `/v0/management/max-retry-interval` | `PutMaxRetryInterval` |
| `PUT` | `/v0/management/max-retry-interval` | `PutMaxRetryInterval` |
| `GET` | `/v0/management/model-definitions/:channel` | `GetStaticModelDefinitions` |
| `GET` | `/v0/management/oauth-callback` | `GetOAuthCallback` |
| `POST` | `/v0/management/oauth-callback` | `PostOAuthCallback` |
| `DELETE` | `/v0/management/oauth-excluded-models` | `DeleteOAuthExcludedModels` |
| `GET` | `/v0/management/oauth-excluded-models` | `GetOAuthExcludedModels` |
| `PATCH` | `/v0/management/oauth-excluded-models` | `PatchOAuthExcludedModels` |
| `PUT` | `/v0/management/oauth-excluded-models` | `PutOAuthExcludedModels` |
| `DELETE` | `/v0/management/oauth-model-alias` | `DeleteOAuthModelAlias` |
| `GET` | `/v0/management/oauth-model-alias` | `GetOAuthModelAlias` |
| `PATCH` | `/v0/management/oauth-model-alias` | `PatchOAuthModelAlias` |
| `PUT` | `/v0/management/oauth-model-alias` | `PutOAuthModelAlias` |
| `DELETE` | `/v0/management/oauth-session` | `CancelAuthSession` |
| `DELETE` | `/v0/management/openai-compatibility` | `DeleteOpenAICompat` |
| `GET` | `/v0/management/openai-compatibility` | `GetOpenAICompat` |
| `PATCH` | `/v0/management/openai-compatibility` | `PatchOpenAICompat` |
| `POST` | `/v0/management/openai-compatibility` | `PostOpenAICompat` |
| `PUT` | `/v0/management/openai-compatibility` | `PutOpenAICompat` |
| `GET` | `/v0/management/plugin-store` | `ListPluginStore` |
| `POST` | `/v0/management/plugin-store/:id/install` | `InstallPluginFromStore` |
| `GET` | `/v0/management/plugins` | `ListPlugins` |
| `GET` | `/v0/management/plugins/routes` | `ListPluginRoutes` |
| `DELETE` | `/v0/management/plugins/:id` | `DeletePlugin` |
| `GET` | `/v0/management/plugins/:id/config` | `GetPluginConfig` |
| `PATCH` | `/v0/management/plugins/:id/config` | `PatchPluginConfig` |
| `PUT` | `/v0/management/plugins/:id/config` | `PutPluginConfig` |
| `PATCH` | `/v0/management/plugins/:id/enabled` | `PatchPluginEnabled` |
| `DELETE` | `/v0/management/proxy-url` | `DeleteProxyURL` |
| `GET` | `/v0/management/proxy-url` | `GetProxyURL` |
| `PATCH` | `/v0/management/proxy-url` | `PutProxyURL` |
| `PUT` | `/v0/management/proxy-url` | `PutProxyURL` |
| `GET` | `/v0/management/quota-exceeded/switch-preview-model` | `GetSwitchPreviewModel` |
| `PATCH` | `/v0/management/quota-exceeded/switch-preview-model` | `PutSwitchPreviewModel` |
| `PUT` | `/v0/management/quota-exceeded/switch-preview-model` | `PutSwitchPreviewModel` |
| `GET` | `/v0/management/quota-exceeded/switch-project` | `GetSwitchProject` |
| `PATCH` | `/v0/management/quota-exceeded/switch-project` | `PutSwitchProject` |
| `PUT` | `/v0/management/quota-exceeded/switch-project` | `PutSwitchProject` |
| `GET` | `/v0/management/request-error-logs` | `GetRequestErrorLogs` |
| `GET` | `/v0/management/request-error-logs/:name` | `DownloadRequestErrorLog` |
| `GET` | `/v0/management/request-log` | `GetRequestLog` |
| `PATCH` | `/v0/management/request-log` | `PutRequestLog` |
| `PUT` | `/v0/management/request-log` | `PutRequestLog` |
| `GET` | `/v0/management/request-log-by-id/:id` | `GetRequestLogByID` |
| `GET` | `/v0/management/request-retry` | `GetRequestRetry` |
| `PATCH` | `/v0/management/request-retry` | `PutRequestRetry` |
| `PUT` | `/v0/management/request-retry` | `PutRequestRetry` |
| `POST` | `/v0/management/reset-quota` | `ResetQuota` |
| `GET` | `/v0/management/routing/adaptive` | `GetAdaptiveRouting` |
| `PATCH` | `/v0/management/routing/adaptive` | `PutAdaptiveRouting` |
| `PUT` | `/v0/management/routing/adaptive` | `PutAdaptiveRouting` |
| `GET` | `/v0/management/routing/adaptive/scores` | `GetAdaptiveRoutingScores` |
| `GET` | `/v0/management/routing/strategy` | `GetRoutingStrategy` |
| `PATCH` | `/v0/management/routing/strategy` | `PutRoutingStrategy` |
| `PUT` | `/v0/management/routing/strategy` | `PutRoutingStrategy` |
| `GET` | `/v0/management/usage-queue` | `GetUsageQueue` |
| `GET` | `/v0/management/usage-statistics-enabled` | `GetUsageStatisticsEnabled` |
| `PATCH` | `/v0/management/usage-statistics-enabled` | `PutUsageStatisticsEnabled` |
| `PUT` | `/v0/management/usage-statistics-enabled` | `PutUsageStatisticsEnabled` |
| `DELETE` | `/v0/management/vertex-api-key` | `DeleteVertexCompatKey` |
| `GET` | `/v0/management/vertex-api-key` | `GetVertexCompatKeys` |
| `PATCH` | `/v0/management/vertex-api-key` | `PatchVertexCompatKey` |
| `POST` | `/v0/management/vertex-api-key` | `PostVertexCompatKey` |
| `PUT` | `/v0/management/vertex-api-key` | `PutVertexCompatKeys` |
| `POST` | `/v0/management/vertex/import` | `ImportVertexCredential` |
| `GET` | `/v0/management/ws-auth` | `GetWebsocketAuth` |
| `PATCH` | `/v0/management/ws-auth` | `PutWebsocketAuth` |
| `PUT` | `/v0/management/ws-auth` | `PutWebsocketAuth` |
| `DELETE` | `/v0/management/xai-api-key` | `DeleteXAIKey` |
| `GET` | `/v0/management/xai-api-key` | `GetXAIKeys` |
| `PATCH` | `/v0/management/xai-api-key` | `PatchXAIKey` |
| `POST` | `/v0/management/xai-api-key` | `PostXAIKey` |
| `PUT` | `/v0/management/xai-api-key` | `PutXAIKeys` |
| `GET` | `/v0/management/xai-auth-url` | `RequestXAIToken` |

## LLM 禁改路由与代码边界

冻结路由：`/v1/models`、`/v1/chat/completions`、`/v1/completions`、`/v1/images/*`、`/v1/videos*`、`/v1/messages*`、`/v1/responses*`、`/v1/alpha/search`、`/openai/v1/videos*`、`/backend-api/codex/responses*`、`/v1beta/models*`、`/v1beta/interactions`，以及管理探测代理 `/v0/management/api-call` 的请求/响应/token refresh 行为。

冻结代码：`sdk/api/handlers/openai/**`、`sdk/api/handlers/claude/**`、`sdk/api/handlers/gemini/**`、`sdk/api/handlers/handlers.go` 转换/流式链、`sdk/cliproxy/auth/conductor.go` executor/selector/runtime credential group、LLM 路由注册和认证中间件。
