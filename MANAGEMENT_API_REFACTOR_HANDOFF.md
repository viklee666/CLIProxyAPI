# Management API 全量重构交接文档

## 目标

扫描并优化 `management.html` 直接或间接使用的全部管理接口。允许重构仅供管理后台使用的路由、请求/响应协议、查询、分页、缓存、持久化和前端调用；禁止修改任何 LLM 请求接口及其运行时调用链。

最终部署方式必须保持为：

```bash
git pull
sudo docker compose up -d --build
```

本次新增的数据库迁移、索引、初始化或生成步骤必须由镜像入口、应用启动或 Compose 生命周期自动且幂等执行，不允许要求用户额外运行命令。

## 硬边界

- [x] 完整列出 `management.html` 直接或间接使用的每个管理接口。
- [x] 定义并冻结全部 LLM API 与运行时主链，后续通过 diff 复核。
- [x] 优化所有大请求、大响应、列表和分页类管理接口；唯一未改协议的是用户明确冻结的 `/api-call`。
- [x] 所有可查询持久状态使用现有 SQLite/配置存储，不引入写死 JSON。
- [x] 客户端分组凭证分配支持“全部凭证”。
- [x] 客户端分组凭证分配支持“某订阅等级的全部凭证”。
- [x] 客户端分组凭证分配支持指定 AI provider。
- [x] 所有部署前置步骤自动、幂等，并兼容现有数据库。

## TODO（严格按顺序推进）

### 0. 交接与审计基线

- [x] 创建本交接文档。
- [x] 确认目标仓库为 `E:\MyCPA\CLIProxyAPI`，审计开始时工作树干净。
- [x] 记录技术栈、启动入口、持久层、迁移机制与测试命令。
- [x] 确认真实 `management.html` 的构建/加载链与前端 API 基址。
- [x] 完成前端全部静态调用入口审计，确认无 WebSocket/EventSource 隐藏入口。
- [x] 完成核心 Go 管理路由的逐 method/path 清单。
- [x] 完成 Manager Plus 自有路由的逐 method/path 清单。
- [x] 将动态插件运行时路由单列，并设计可查询 route inventory。
- [x] 为每个接口族补齐调用者、数据源、现有边界、风险与最终处置，逐路由附录确保无遗漏。
- [x] 将核心 152 项与 Manager Plus 35 项逐 method/path 标为已优化、天然有界或仍有风险。
- [x] 建立 LLM 禁改路由与代码边界清单。

### 1. 统一设计

- [x] 定义统一分页/游标协议、上限与兼容策略。
- [x] 定义服务端筛选、排序、字段投影、批处理和响应大小上限。
- [x] 定义无需预拉全部 ID 的凭证集合选择语义：显式选择或服务端 query selector，并支持排除项。
- [x] 定义订阅等级与 AI provider 作为 selector 条件，不修改 LLM 凭证真源。
- [x] 确定复用 `client-access.sqlite` 和启动时自动迁移，不新增人工部署步骤。
- [x] 定义管理凭证目录/索引同步方式：复用 auth manager 真源与 2 秒可失效目录缓存，不建立平行数据库真源。
- [x] 定义并执行现有数据库升级与全新数据库初始化测试矩阵。

### 2. 实现

- [x] A：完成 Client Access 查询 N+1、批量绑定和 reload 热点优化。
- [x] B：实现全部凭证/订阅等级/provider 的服务端 selector 与兼容响应。
- [x] C：更新客户端分组页面的跨页选择、provider 筛选与结果反馈。
- [x] D：重构 auth-files 列表、增量快照和批处理接口。
- [x] E：重构 provider/config/API key 等整表读写热点。
- [x] F：重构日志、错误日志、usage queue、普通上传/删除等无界请求；`/api-call` 按 LLM/探测冻结边界排除。
- [x] G：重构插件清单、插件商店与动态插件管理路由的大请求/轮询热点。
- [x] H：重构 Manager Plus usage、monitoring、dashboard、inspection、account-action 等大数据接口。
- [x] H1：`POST /v0/management/monitoring/analytics` 的 model/account/credential/api-key 聚合改为独立实体级分页，旧 boolean/limit 兼容且有默认/硬上限。
- [x] H2：`GET /v0/management/account-action-candidates` 改为服务端搜索与分页，Usage Analytics/账户操作候选前端不再预拉全量聚合列表。
- [x] I：为仍需列表返回的全部管理接口补齐上限、分页/游标、筛选或聚合路径。
- [x] J：增加所需持久化复合索引并接入自动幂等迁移。
- [x] K：将全部部署前置步骤接入自动容器启动。

### 3. 验证

- [x] 根项目 Go 单元/集成测试通过。
- [x] Manager Server Go 测试通过（最终源码 Linux 全包）。
- [x] 前端测试、类型检查、lint 与构建通过。
- [x] 每个管理接口完成回归核对（核心 152、Manager 35、动态插件 inventory）。
- [x] 大数据、深分页、跨页全选和并发批处理场景通过。
- [x] 现有数据库自动升级通过。
- [x] 全新数据库自动初始化通过。
- [x] Docker Compose 构建/启动路径通过。
- [x] 最终 diff 证明 LLM API、转换器、executor、selector 主链未修改。

## 已确认的运行时基线

- 前端源码：`extensions/cpa-manager-plus/apps/web/src`，React 19 + TypeScript + Vite。
- Docker 构建把 Web `dist/index.html` 改名并嵌入为真实 `management.html`；仓库中的 14 行 HTML 只是占位文件。
- 浏览器所有 `apiClient` 相对路径最终落到 `{apiBase}/v0/management/...`。
- 集成镜像同时启动 `CLIProxyAPI`（8317）和 `cpa-manager-plus`（18317）；Manager Plus 专属路由在内部处理，其余管理路由反代到核心服务。
- Client Access 使用 `/data/client-access.sqlite`；Manager Plus usage 使用 `/data/usage.sqlite`。
- 两套 SQLite schema/index 均在应用启动时自动幂等迁移，符合只执行 Compose 重建的部署要求。
- 普通 provider/API key/config 仍以运行内存配置 + YAML 落盘，这是 LLM 运行配置真源；只优化其管理读写协议，不迁移或改动 LLM 执行链。

## 当前共享工作树/WIP 归属

- `deploy_group_audit` 子任务已完成客户端分组完整包，改动限定于 `internal/clientaccess/**`、管理 handler/router、`ClientGroupsPage`/`ClientKeysPage`、对应 API/测试。
- `internal/clientaccess/{store,service,types}.go` 已完成 N+1 消除、集合式批量绑定、真实变更统计与 1200 凭证大集合验证。
- auth-files snapshot ETag 与前端同步改动已存在于工作树，需在 D 批次继续审查和测试。
- 其他子任务只读审计，不应修改共享文件；主任务避免覆盖上述 WIP。

## 工作日志

### 2026-07-15

- [x] 创建持久目标与初始 TODO。
- [x] 确认最终用户操作仅为 `git pull` 后执行 `sudo docker compose up -d --build`。
- [x] 定位目标仓库、真实 Web 源码、构建产物与双进程反代关系。
- [x] 确认两套 SQLite 数据库和自动迁移入口。
- [x] 记录验证命令：根项目 `go test ./...`、Manager Server `go test ./...`，前端 `npm test`、`npm run type-check`、`npm run lint`、`npm run build`。
- [x] 完成前端静态调用入口审计，约 120 个 method/path 组合，另含 `/api-call` 动态目标和两个浏览器直连外部接口；未发现 WebSocket/EventSource 请求入口。
- [x] 确认客户端分组根因：选择状态只保存当前页显式 `auth_indices`，保存协议不支持服务端集合选择，且 UI 没有 provider 条件。
- [x] 确认首批核心热点：auth-files 伪后端分页/周期全扫、Client Access N+1 与逐条 SQL、provider 整表 GET+PUT、日志和 usage queue 无界参数、插件全表轮询、monitoring 高维原始扫描。
- [x] 接管并核对当前工作树中的首批 WIP，明确并行文件归属，避免覆盖。
- [x] 完成 auth-files 增量目录指纹：`snapshot_etag` 能识别新增、删除、provider/plan 身份变化；正常状态变化继续使用 `updated_after_ms` 小增量。
- [x] 将复杂凭证筛选的无分页回退从全量 `detail` 改为分页 `summary` 聚合，避免携带重字段。
- [x] 完成插件变更轮询第一批优化：`/plugins`、`/plugin-store` 支持按目标 ID（商店额外支持 source）查询，轮询指数退避并在结束时仅做一次全量刷新。
- [x] 为插件商店注册表加入 5 秒成功缓存/2 秒失败缓存，避免安装期间每 500ms 重下载完整 registry。
- [x] 插件商店多 source 拉取改为最多 4 个 worker，release enrichment 改为最多 8 个 worker；新增 `GET /plugins/routes` 返回稳定排序的运行时动态管理路由 inventory。
- [x] 上述批次定向验证通过：Go handler 测试、前端 36 项测试、TypeScript 类型检查。
- [x] 完成 AI provider 单记录创建/更新协议：Gemini、Interactions、Codex、Claude、Vertex、OpenAI compatibility 新增改用单条 `POST`，编辑改用单条 `PATCH`，不再为一条记录执行 `GET /config + PUT` 全数组；支持 `api-key + base-url` 或 `auth-index` 稳定匹配，OpenAI 使用原始 name 匹配，并可显式清空数组/headers/Cloak 等字段。
- [x] Provider 单记录批次定向验证通过：Go create/auth-index patch/false-field 响应测试、前端 providers 17 项测试及 TypeScript 类型检查。
- [x] 为破坏性 `GET /usage-queue` 增加 `count <= 1000` 上限；非法请求在弹出队列前返回 400，现有默认值和 Manager Plus batch=100 保持兼容。
- [x] 为 `PUT /config.yaml` 增加 16 MiB 请求体上限；超限返回 413，不再无界 `io.ReadAll`。
- [x] 为运行时插件管理路由增加 4 MiB 请求体和 16 MiB 响应体上限；超限数据不会进入插件 handler 或写回客户端。
- [x] `GET /logs` 空 `limit` 改为默认 1000、最大 10000；前端显式 10000 缓冲协议保持兼容。
- [x] `GET /request-error-logs` 增加 `page/page_size/search/view=count`、total/has_more 元数据与最大 page_size=200。
- [x] 错误日志列表和 `request-log-by-id` 复用 5 秒目录 catalog；Dashboard 改用 count 视图，日志页改用每页 100 的服务端分页。
- [x] 新增 `POST /v0/management/client-access/credential-bindings/bulk`：基于一次 `authManager` 快照解析 `all/providers/plan_types/excluded_auth_indices`，固化精确 `auth_index`，不建立未来自动入组规则；旧 PUT 显式 ID 协议保持兼容。
- [x] Client Access Store 的 group 计数、key/group/reservation 关系读取改为分页 CTE/分块批量查询；binding replace 改为去重、差异比较、集合删除和批量插入，并返回真实 `matched/updated/unchanged`。
- [x] 客户端分组页面支持全选所有、全选当前订阅等级、AI provider 多选与当前页 exclusions；预计范围由服务端 dry-run 计算，保存过程不拉取全量 ID；groups 与 bindings 均循环读取完整分页。
- [x] Gemini/Interactions/Codex/Claude/XAI/Vertex/OpenAI compatibility 增加单记录 POST；管理前端创建不再 GET 完整 config 后 PUT 整段数组。
- [x] Provider PATCH 补齐 priority/models/headers/disable-cooling 等可清空字段，并支持 `match-auth-index + match-base-url` 精确定位；旧 PUT 整段替换继续兼容。
- [x] 新增无密钥内容的 `GET /config/summary`；Dashboard 用 1 个计数请求替代 API keys + 4 组 provider 全数组请求。
- [x] 完成当前树的全量后端路由快照：核心静态 152 项、Manager Plus 自有 35 项、插件动态集合；逐项附录与接口族性能处置矩阵已写入独立文档。
- [x] Auth Files 多文件上传改为单个 multipart 请求；新增批量状态、批量字段修改和流式 ZIP 下载接口，管理页面不再发 N 个 PATCH、约 2N 个下载/回传请求或 N 个顺序下载请求；各批量入口均有 1000 目标硬上限与逐项失败结果。
- [x] Auth Files 无参数旧列表也改为默认 50、最大 500 的有界分页；manager 候选目录与 identity ETag 使用 2 秒可失效缓存，同一轮多页/筛选不再重复 clone/构建全量凭证，上传、删除、状态和字段变更会立即失效缓存。
- [x] Auth Files 带 `updated_after_ms` 的增量查询绕过 2 秒候选缓存并读取实时状态，避免客户端推进 cursor 后漏掉缓存窗口内的运行时更新；无 cursor 的分页/筛选仍复用缓存。
- [x] Auth JSON 单文件限制 16 MiB、multipart 总请求 128 MiB、批量 ZIP 原始总量 128 MiB、批量 JSON 请求 2 MiB；单文件下载改为流式传输。
- [x] Manager Plus analytics 四类高维聚合完成实体级 SQL `LIMIT/OFFSET`：请求使用独立 `*_stats_page {page,limit}`，响应返回 `*_stats_pagination {page,limit,total,count,has_more,truncated}`；旧 boolean 默认每页 50，硬上限 200，旧 account/api limit 继续兼容。
- [x] Analytics summary 不再通过 `limit=0` 聚合数组计算成本/缓存命中率，改为每批 200 个 model 实体流式累加；legacy `filter_options` 聚合也限制为首批 50 并返回分页元数据。
- [x] Usage Analytics Overview/Trends 仅请求实际展示的 top 8/top 4；Models/API Keys/Credentials 主列表使用每页 25 的服务端分页，API key 专用关键词在分页前由服务端搜索，选中 API key/credential 的独立趋势请求保持不变。
- [x] Account Action Candidates 支持 `page/page_size/search/status`，返回 `items,total,page,page_size,pendingCount`；旧 `limit` 映射到第一页 page size（最大 200），前端改为 350ms 防抖服务端搜索和翻页。
- [x] 日常 `GET /config?view=ui` 改为有界 bootstrap 投影：每类配置最多 200 条轻量身份记录并返回完整 totals/truncated 元数据，移除 provider 重模型/headers、payload 与插件私有配置；无参数/full 兼容读取设 32 MiB 响应上限。
- [x] Web 常规 config bootstrap 使用 `view=ui`；provider legacy 整段保存为保留未知字段时改从各自分页接口续页读取，OpenAI `disable-cooling` 不再回退下载完整 `/config`。
- [x] 完成部署链路只读验收：本次 Dockerfile/Compose/entrypoint/两套 migrate 文件无 diff；Compose 配置、完整镜像构建、全新 tmpfs 数据目录启动及现有/legacy SQLite 迁移测试均通过，没有新增人工前置命令。
- [x] 修正 `docs/*` 忽略规则，为路由 inventory 与逐路由三态附录增加显式例外，确保两份审计文档进入 Git 交付而不是只存在于本地工作区。

## 接口清单

完整清单见 [docs/MANAGEMENT_API_ROUTE_INVENTORY_CN.md](docs/MANAGEMENT_API_ROUTE_INVENTORY_CN.md)：当前核心 152 个静态 method/path、Manager Plus 35 个自有 method/path，并单列插件运行时动态集合。下面保留功能分组索引：

1. 核心配置、YAML、布尔/数值开关、routing/adaptive。
2. API keys 与 Gemini/Interactions/Codex/Claude/Vertex/OpenAI compatibility provider 配置。
3. auth-files、OAuth 模型排除/别名、模型定义、上传/下载/批量状态和字段修改。
4. Client Access groups、keys、credential-bindings。
5. logs、request-error-logs、request-log-by-id。
6. plugins、plugin-store 与插件运行时注册管理路由。
7. OAuth、Vertex import、版本检查与 `/api-call` 管理代理。
8. Manager Plus info/setup/config/status、usage import/export、model prices、aliases、account actions、Codex inspection、dashboard、monitoring analytics/header snapshots、quota cooldowns。

## LLM 禁改边界

冻结路由：

- `/v1/models`
- `/v1/chat/completions`
- `/v1/completions`
- `/v1/images/*`
- `/v1/videos*`
- `/v1/messages`
- `/v1/messages/count_tokens`
- `/v1/responses*`
- `/v1/alpha/search`
- `/openai/v1/videos*`
- `/backend-api/codex/responses*`
- `/v1beta/models*`
- `/v1beta/interactions`
- `/v0/management/api-call` 的请求、响应、token refresh 与模型/配额探测行为

冻结代码主链：

- `sdk/api/handlers/openai/**`
- `sdk/api/handlers/claude/**`
- `sdk/api/handlers/gemini/**`
- `sdk/api/handlers/handlers.go` 的请求转换/流式处理链
- `sdk/cliproxy/auth/conductor.go` 的 executor、selector 与 credential group 运行时筛选
- `internal/api/server.go` 的 LLM 路由注册和认证中间件

客户端分组的新选择语义只能改变管理端目录与绑定写入；不得改变以上协议、转换器、执行器或运行时选择逻辑。

## 设计决策

- 复用现有 Client Access SQLite 和自动启动迁移，不建立平行持久系统。
- 可扩展批量分配使用服务端解析的 selection expression，支持 `all`、plan/provider 条件和排除项，同时兼容显式 `auth_indices`；搜索与“显示分组”只影响浏览列表，不会隐式改变跨页选择集合。
- auth-files 仍是 LLM 凭证真源；如增加管理目录表，它只能是可重建的查询索引。
- 列表优先服务端筛选/排序/投影；深分页优先游标，必须保留旧协议时设置安全上限并提供兼容元数据。
- 大批量写入必须在单事务内使用集合 SQL/分块参数，避免逐项 delete/insert 和每次写后全量 reload。
- 全部请求体、文件、日志、代理响应和动态插件请求增加明确上限；用户显式导入/导出使用流式处理。
- 所有 LLM 路由与 runtime executor/translator 均排除在改动集外。

## 变更日志

- 完成：Client Access `ListGroups` 改为页面 CTE + 聚合计数，避免每行相关子查询。
- 完成：Client Access `ListKeys`/全量 reload 改为分块批量加载 group 与 reservation，移除约 `1+2N` 查询。
- 完成：credential binding 替换改为一次事务内分块查询、差异比较、集合删除、批量插入，并返回 matched/updated/unchanged；新增 query selector 与 exclusions 管理接口。
- 完成：Client Groups 页面新增全量/订阅等级/provider 跨页选择、服务端预计命中反馈；当前页反选仅发送 exclusions，分组和绑定关系不再受 200 条上限静默截断。
- 完成：auth-files snapshot 增加与顺序无关的 identity ETag；前端仅在目录身份变化时补一次全量，旧后端仍保留五分钟兼容刷新。
- 完成：Auth Files 复杂本地筛选回退改用分页轻量 summary，不再调用无参数全量 detail。
- 完成：插件与插件商店轮询改为目标查询、指数退避、结束后一次全量刷新；registry 增加短 TTL 缓存。
- 完成：插件 registry/release 外部查询使用有界 worker pool；动态插件 Management API 可通过 `/plugins/routes` 枚举当前实际加载的 method/path/plugin_id/version。
- 完成：Provider 管理 CRUD 从整份配置读改写切换为记录级 `POST/PATCH`；保留旧 `PUT` 仅作兼容批量入口，正常管理页面不再触发整表写回。
- 完成：`GET /usage-queue` 增加最大批量 1000 的硬边界，防止单次无界弹出和大 JSON 响应。
- 完成：新增普通管理请求/响应有界读取工具；配置 YAML 等非探测接口超限时返回结构化错误。
- 完成：动态插件管理请求由无界 `io.ReadAll` 改为 `MaxBytesReader`，并为插件响应增加写回前硬上限。
- 完成：日志读取默认/最大行数有界；错误日志响应分页并缓存目录索引；Dashboard 不再为计数下载全部错误日志元数据。
- 完成：主要 provider 创建/编辑切换为记录级 POST/PATCH，移除前端每次修改前的全量 `/config` 读取与整段覆盖写入。
- 完成：Dashboard 配置统计改用轻量 summary，只返回各配置段数量，不序列化 API key/provider 记录。
- 完成：Auth Files `POST /auth-files` 复用后端多文件 multipart；新增 `PATCH /auth-files/status/batch`、`PATCH /auth-files/fields/batch` 和 `POST /auth-files/download`（ZIP 流），保留全部单项接口兼容旧客户端。
- 完成：Auth Files 候选目录/ETag 短 TTL 索引覆盖多页读取并在所有管理写操作后失效；旧无参数 GET 不再返回无界 detail。
- 完成：Auth Files 增量 cursor 查询改为实时扫描，消除短 TTL 候选缓存与 `updated_after_ms` 推进之间的漏更新窗口。
- 完成：Manager Plus monitoring analytics 的 model/account/credential/api-key 聚合按最终实体而非原始 model 子行分页，避免实体被拆页、total 失真和无界数组；没有修改 events cursor、dashboard summary、Codex inspection 或任何 LLM/provider 调用接口。
- 完成：Account Action Candidates 从一次拉取 200 条后本地筛选改为数据库端 status/search/count + `LIMIT/OFFSET`，页面展示全局 total/pendingCount 和服务端分页。
- 完成：部署保持零人工迁移；Client Access 与 Manager usage SQLite 继续由应用启动自动、幂等初始化/升级，镜像入口无需增加额外命令。
- 完成：完整 `/config` 从后台日常必拉大响应降为显式兼容路径；常规 UI 只取有界字段投影，大配置集合继续使用各自分页 API。

## 验证日志

- PASS：`go test ./internal/api/handlers/management -run 'Test(AuthFileSnapshotETag|ListAuthFilesQueryReturnsOnlyStatusesUpdatedAfterCursor|ListPluginsFiltersPollingResponseByID|ListPluginStoreFiltersPollingResponseAndCachesRegistry)' -count=1`。
- PASS：前端 `authFiles`、`useAuthFilesData`、`pluginPolling` 定向测试，共 36 项。
- PASS：插件 registry/release bounded worker、`ListPluginRoutes` 与 `ManagementRouteInventory` 定向 Go 测试。
- PASS：前端 `npm --workspace apps/web run type-check`。
- PASS：`go test ./internal/api/handlers/management -run 'Test(PostGeminiKey|PatchCodexKeyMatchesAuthIndex|GetOpenAICompat)' -count=1`。
- PASS：前端 `providers.test.ts` 17 项与 `npm --workspace apps/web run type-check`。
- PASS：`go test ./internal/api/handlers/management -run "TestGetUsageQueue" -count=1`。
- PASS：`go test ./internal/api/handlers/management -run "TestPutConfigYAMLRejectsOversized" -count=1`。
- PASS：`go test ./internal/pluginhost -run "TestServeManagementHTTPRejectsOversized" -count=1`。
- PASS：`go test ./internal/api/handlers/management -run "Test(GetLogs|GetRequestErrorLogs|GetRequestLogByID)" -count=1`。
- PASS：前端 `src/services/api/logs.test.ts` 5 项测试与 `npm --workspace apps/web run type-check`。
- PASS：Provider 记录级 Go 定向测试 3 项；前端 `src/services/api/providers.test.ts` 17 项。
- PASS：`TestGetConfigSummaryReturnsCountsWithoutRecords`；前端 `src/services/api/config.test.ts` 与 TypeScript 类型检查。
- PASS：`go test ./internal/clientaccess -count=1`，覆盖 1200 凭证 × 3 分组批量写、重复 no-op、优先级变更、无效组回滚及批量关系读取。
- PASS：`go test ./internal/api/handlers/management -run "Test(BulkReplaceClientAccessCredentialBindings|ClientAccessManagement)" -count=1`。
- PASS：根项目 `go test ./...` 与 `go build -o test-output.exe ./cmd/server`。
- PASS：前端 Client Access 定向 2 个测试文件共 6 项，`type-check`、`lint`、生产构建通过。
- PASS：`go test ./internal/api/handlers/management -run 'Test(PatchAuthFile(Status|Fields)Batch|DownloadAuthFilesBatch|UploadAuthFile_Batch)' -count=1`。
- PASS：前端 `authFiles.test.ts` + `useAuthFilesData.test.ts` 共 36 项及 TypeScript 类型检查。
- PASS：Auth Files 默认分页、候选缓存复用/失效、disk 字段兼容及全部既有 query/status/fields/upload/download 定向测试。
- PASS：`go test ./internal/api/handlers/management -run 'Test(ListAuthFiles|AuthFileSnapshotETag|CachedAuthFileCandidates)' -count=1`，覆盖 Auth Files 实时增量查询与候选缓存回归。
- PASS：Config UI projection/full compatibility/response limit Go 定向测试；前端 `config.test.ts` + `providers.test.ts` 共 20 项与 TypeScript 类型检查。
- 最终全量验证已完成；本节之前记录的定向测试均已纳入最终根 Go、Manager Linux、Web 与镜像构建回归。

### 2026-07-16：API key usage 分页批次

- [x] `GET /api-key-usage` 从无界 provider→key 嵌套对象改为稳定排序的扁平分页响应，默认 `page_size=100`、硬上限 200，并返回 `total/page/page_size/total_pages/has_more`。
- [x] 增加 `provider`（支持逗号或重复参数）与 `search` 服务端筛选；先聚合和分页，再仅为当前页生成 recent-request 时间桶，页外记录不再构造 20 桶明细。
- [x] 管理前端 API 客户端按每页 200 循环读取，单次响应始终有界；保留旧嵌套响应归一化以兼容滚动升级，provider 页面缓存/展示语义不变。
- PASS：`go test ./internal/api/handlers/management -run "TestGetAPIKeyUsage" -count=1`。
- PASS：前端 `src/services/api/apiKeyUsage.test.ts` 4 项与 `npm --workspace apps/web run type-check`。

### 2026-07-16：Adaptive routing scores 分页批次

- [x] `GET /routing/adaptive/scores` 保留既有 provider/model 过滤，在管理 handler 层增加默认 `page_size=100`、硬上限 200 的分页和 `total/page/page_size/total_pages/has_more` 元数据。
- [x] 增加 `search`（auth ID/auth index/provider）与 `eligible=true|false` 服务端筛选；未修改 `sdk/cliproxy/auth` selector、executor 或任何 LLM 路由/执行链。
- [x] Adaptive Routing 管理页按 100 条服务端分页并提供前后翻页；API 客户端兼容旧无分页 snapshot 响应。
- PASS：`go test ./internal/api/handlers/management -run "TestAdaptiveRouting" -count=1`。
- PASS：前端 `src/services/api/adaptiveRouting.test.ts` 2 项与 `npm --workspace apps/web run type-check`。

### 2026-07-16：Config/API keys/OAuth/模型定义有界集合批次

- [x] `GET /api-keys` 增加默认 `page_size=100`、硬上限 200、稳定 page 元数据和 `search`；新增单记录 `POST /api-keys`，旧 PUT/PATCH/DELETE 兼容保留。
- [x] Dashboard/System 的模型探测回退改为 `page_size=1` 只取首个 management key，不再下载完整 key 数组；`apiKeysApi.list()` 对分页后端自动续页并兼容旧全量响应。
- [x] Gemini、Interactions、Codex、Claude、XAI、Vertex、OpenAI compatibility 的 GET 列表统一为默认 100/最大 200；前端 provider API 检测 `has_more` 后按服务端 page size 循环续页，避免超过 100 条静默截断。
- [x] 上述 config 集合的 PUT/PATCH/POST 请求统一限制为 4 MiB，完整替换/新增集合限制为最多 10,000 条；正常 UI 继续使用记录级 POST/PATCH，旧整段 PUT 仅作兼容批量入口。
- [x] `GET /oauth-excluded-models` 与 `GET /oauth-model-alias` 按模型/alias 扁平条目分页，支持 provider/channel 与 search 过滤；同一 provider/channel 可跨页，前端自动合并 map。
- [x] `GET /model-definitions/:channel` 增加默认 100/最大 200 分页、search 与 total/has_more；前端自动续页，旧无分页响应兼容。
- [x] `POST /oauth-callback` 请求体限制 64 KiB，并限制 redirect/code/error 字段长度；`POST /vertex/import` 整体 multipart 限 2 MiB、service-account 文件限 1 MiB。
- [x] `GET /latest-version` 的 GitHub 成功响应限制 256 KiB，错误响应原有 1 KiB 摘要限制保持不变。
- PASS：Go config collection/OAuth/model definitions/Vertex/latest-version 定向测试。
- PASS：前端 `apiKeys`、`providers`、`authFiles` 3 个测试文件共 39 项及 TypeScript 类型检查。

### 2026-07-16：剩余路由三态审计与默认请求总闸

- [x] 新增独立逐路由处置附录 `docs/MANAGEMENT_API_DISPOSITION_CN.md`；核心 152/152、Manager Plus 35/35 均精确标为已优化、天然有界或仍有风险。
- [x] 通过脚本核对 `internal/api/server.go` 实际核心 route set、inventory 表与三态附录：三者均为 152，method/path 双向差集均为空；Manager inventory 与三态附录均为 35，双向差集为空。
- [x] 核心 management group 增加默认 4 MiB 请求体总闸；`POST /api-call` 明确跳过且测试证明不受影响，auth-files upload 与 config.yaml 继续使用各自更大的专用上限。
- [x] `GET /config.yaml` 从 `os.ReadFile` 改为带 16 MiB 硬边界的流式响应；兼容已有配置，超限返回 413。
- [x] AI Providers 主列表改为直接调用六组已分页 provider endpoint，不再为列表页额外下载完整 `/config`。
- PASS：默认请求总闸/API-call 排除、config YAML 下载、config collection、OAuth/Vertex/version 定向 Go 测试。
- PASS：AI Providers 调整后的 TypeScript 类型检查。

### 2026-07-16：Manager model-prices/API-key-aliases 有界协议

- [x] `GET /v0/management/model-prices` 与 `GET /v0/management/api-key-aliases` 改为 SQLite 直接 `page/page_size/search`，默认 100、最大 200；前端兼容旧全量响应并自动续页合并。
- [x] model-prices 兼容 PUT 保留整表替换语义，但统一 4 MiB 请求体、最多 10,000 项，保存后不再重新 LoadAll；aliases PUT 最多 1,000 项/active hashes 5,000，只回传本次变更项，前端在本地合并并清理同名 orphan。
- [x] model price sync 请求最多 500 个模型；LiteLLM/OpenRouter 上游 JSON 各限 32 MiB；响应不再携带整库 prices，RawJSON 从 matched/candidates 投影移除，前端随后通过分页 GET 重载。
- PASS：Manager modelprice/apikeyalias controller/service/repository 定向 Go 测试。
- PASS：前端 `usageService.pagination.test.ts` 6 项及 TypeScript 类型检查。
- PASS：本批次组合回归：日志/API key usage/adaptive Go 定向测试；3 个前端 API 测试文件共 11 项；本批次 TypeScript/TSX 文件 ESLint。

### 2026-07-16：Manager Plus analytics/account-action 分页批次

- PASS：`go test ./internal/repository/usageevent ./internal/repository/accountaction ./internal/service/monitoring ./internal/service/accountaction ./internal/http/controller/monitoring ./internal/http/controller/accountaction ./internal/httpapi`。
- PASS：Manager Server `go test ./...` 中本批次及其余包均通过；仅 `internal/security.TestLoadOrCreateDataKeyCreatesStableRestrictedFile` 在 Windows 报文件权限 `666, want 600`，与本批次无关。
- PASS：前端 Usage Analytics、Account Action Candidates、Demo 定向 5 个测试文件，共 68 项。
- PASS：前端 `npm --workspace apps/web run type-check`。
- INFO：前端全量 Vitest 共 798 项中 796 通过；2 项失败位于并行修改中的 `useAuthFilesData.test.ts`，原因为 mock 尚未提供新增的 `authFilesApi.patchFieldsBatch`，不涉及本批次文件。

### 2026-07-16：部署与数据库升级验收

- PASS：临时注入 `CPA_MANAGEMENT_KEY` 后执行 `docker compose config --quiet`。
- PASS：`docker compose build cli-proxy-api` 完整通过，覆盖 Web TypeScript/Vite、核心 Linux CGO、Manager Server Linux build 与最终镜像组装。
- PASS：使用全新 tmpfs 数据目录启动镜像，Manager `/health` 返回 200，自动创建 `usage.sqlite`、`client-access.sqlite` 与 `data.key`，核心和 Manager 两进程启动日志正常。
- PASS：Client Access 持久化 reopen、1000 项批量及全包测试；Manager SQLite/store 新库、legacy schema、迁移失败回滚与重试测试。
- INFO：本机未配置生产 `.env` 且不存在 external network `shared_proxy`，因此未对本地真实部署执行 `compose up`；已部署环境保留既有 `.env`/network 即可继续只执行 `git pull` 与 `sudo docker compose up -d --build`。

### 2026-07-16：Manager Plus 剩余无界接口批次（完成）

- [x] legacy `GET /v0/management/usage` 改为稳定的最近事件分页：默认 `page_size=200`、硬上限 500，支持 `page`、旧 `limit` 别名及带快照上界的 keyset `cursor`。
- [x] usage 兼容响应保留 `total_requests/success_count/failure_count/total_tokens/apis`，新增 `page/page_size/cursor/next_cursor/total/total_pages/has_more`；游标页固定首次读取的 `max(id)`，后续新增事件不会导致翻页重复/漂移。
- PASS：`go test ./internal/repository/usageevent ./internal/service/usage ./internal/http/controller/usage ./internal/httpapi`；新增 cursor、旧 `limit`、总数及硬上限覆盖。
- [x] Codex inspection runs 历史增加 `page/page_size/total/total_pages/has_more`（旧 `limit` 仍为 page-size 别名，单页最大 100）；run detail 的 results/logs 分别独立分页、单页最大 200，并可用 `include_results/include_logs` 做投影。
- [x] `POST /codex-inspection/run` 与 `POST /runs/:id/actions` 不再把完整 results/logs 作为单个响应写回；动作 result IDs 单请求硬上限 200。前端批量动作自动按 200 IDs 分块顺序提交并合并 outcomes，再按 results/logs 独立分页续取详情；兼容旧后端无 pagination 元数据的整包响应，页面现有本地筛选/分页语义不变。
- PASS：`go test ./internal/repository/codexinspection ./internal/service/codexinspection ./internal/http/controller/codexinspection ./internal/httpapi -run 'CodexInspection' -count=1`；前端 `npm --workspace apps/web run type-check`。
- [x] `GET /usage-service/quota-cooldowns` 从 active 全表响应改为数据库端 provider 精确过滤、auth file/index 精确过滤、跨 file/index/account/provider/owner 搜索及 `page/page_size`（默认 100、最大 200）；返回 total/total_pages/has_more。
- [x] 前端 cooldown API 按每页 200 自动续页并合并，旧后端未返回分页元数据时只读原整包一次；现有 Auth Files/Quota 页面调用签名保持兼容。
- PASS：`go test ./internal/repository/quotacooldown ./internal/http/controller/quotacooldown ./internal/httpapi -run 'QuotaCooldown' -count=1`；前端 `usageService.pagination.test.ts` 4 项、TypeScript 类型检查及相关 ESLint 通过。
- [x] monitoring `account-history` 在 handler 与 service 双层强制最多 200 targets；`header-snapshots` 强制 `days<=365`、`limit<=2000`，超限请求在查询前返回 400，不再仅静默截断。
- PASS：`go test ./internal/service/monitoring ./internal/http/controller/monitoring ./internal/httpapi -run 'AccountHistory|HeaderSnapshots' -count=1`。
- [x] Manager Server 自有 JSON 控制器统一使用 4 MiB `MaxBytesReader` 解码器，超限（含未知 Content-Length 的流式 body）返回 413，并拒绝同一请求体中的多个 JSON 值；account-history 保留 unknown-field 拒绝，model-price sync 保留空 body 兼容。
- [x] `/v0/management/usage/import` 继续使用既有 64 MiB 流式导入边界；CPA 管理代理、LLM 路由及 `/v0/management/api-call` 未接入该解码器，行为未修改。
- PASS：`go test ./internal/http/response ./internal/http/controller/... ./internal/httpapi -run 'DecodeJSON|BodyLimit|CodexInspection|QuotaCooldown|AccountHistory|HeaderSnapshots|UsagePagination' -count=1`。
- [x] SQLite 启动迁移幂等新增 usage recent、Codex result page、quota cooldown provider/auth 查询索引；无需部署前手工命令，现有 `docker compose up -d --build` 启动过程自动执行。
- PASS：Manager Server 本批相关 repository/service/controller/httpapi 全包定向回归（含 SQLite migration）通过。
- INFO：Manager Server `go test ./...` 除 Windows 上既有 `internal/security.TestLoadOrCreateDataKeyCreatesStableRestrictedFile`（权限显示 666、期望 600）外全部通过；该失败与本批无关，和前一批全量结果一致。
- PASS：前端本批定向 Vitest、TypeScript 类型检查、定向 ESLint 与 `npm --workspace apps/web run build` 生产构建通过。

### 2026-07-16：插件管理集合完整分页

- [x] `GET /v0/management/plugins`、`GET /v0/management/plugin-store`、`GET /v0/management/plugins/routes` 统一支持 `page/page_size`，默认 100、最大 200，并稳定返回 `page/page_size/total/total_pages/has_more`。
- [x] 插件列表按 ID、动态路由按 runtime inventory 稳定顺序、插件商店按 source ID/plugin ID/version 排序后切页；ID/source 目标轮询继续兼容。
- [x] 插件商店先完成筛选、排序与分页，再构建当前页本地状态及 release enrichment；页外插件不会触发 GitHub release 查询，source 重名计数仍基于完整 registry 数据。
- [x] Web 插件服务默认以 `page_size=200` 自动续页合并；旧后端未返回分页元数据时只读取一次，store sources/source errors 去重合并，现有页面与轮询调用签名不变。
- PASS：`go test ./internal/api/handlers/management -count=1`。
- PASS：`go test -race ./internal/api/handlers/management -run 'Test(ListPlugins|PluginCollection|ListPluginRoutes|ListPluginStore)' -count=1`。
- PASS：前端 `plugins.test.ts` 7 项、`npm run type-check` 与相关 ESLint。
- PASS：最终源码根项目 `go test ./... -count=1` 全部通过。
- PASS：最终源码 Manager Server 通过 Linux `service-build` 镜像内 `go test ./... -count=1`；Windows 仅有既有 chmod `0666/0600` 语义差异。
- PASS：最终 Web 全量 Vitest 103 个文件、816 项测试全部通过；TypeScript type-check、全量 ESLint 与生产构建通过。
- PASS：最终 `docker compose config --quiet` 与 `docker compose build cli-proxy-api`；隔离 tmpfs 容器 health=200，自动创建两套 SQLite 与 data key。
- PASS：1200 凭证集合绑定、全量/订阅/provider 跨页 selector、Auth Files 大集合有界页、Manager keyset/聚合分页、插件页外零 enrichment 与 bounded workers 等大数据场景均由最终全量测试覆盖。
- PASS：最终 `git diff --check`；冻结 LLM handlers/translator/executor/selector 与 `/api-call` 实现 diff 均为空，`server.go` 仅包含 management middleware/route 注册改动。
