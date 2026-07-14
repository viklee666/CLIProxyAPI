# Management API 性能扫描结果

## 已改造热点

| 路径 | 原行为 | 当前行为 |
|---|---|---|
| `GET /v0/management/auth-files` | 遍历全部凭证；每项生成 20 个请求桶、解析 Codex JWT、读取文件状态；无服务端分页 | 保留无参数旧行为；带查询参数时支持分页、筛选、排序和 `detail/summary/snapshot/count` 视图 |
| Dashboard 凭证数量 | 下载完整凭证列表后只取 `length` | `view=count&page_size=1`，不生成任何文件详情 |
| Auth Files 页面 | 下载全量后浏览器切片分页 | 普通筛选走服务端分页；只有依赖跨账号 Quota 状态的高级筛选才兼容回退到全量模式 |
| Manager auth snapshot | 每 30 秒下载全部凭证 | 只请求当前 usage batch 涉及的 `auth_index`，并使用 `snapshot` 字段集 |
| Manager 单凭证校验 | 带 `name/auth_index`，但 CLI 后端忽略参数并返回全部 | CLI 后端在查询模式下精确筛选 |
| 请求监控事件 | 单次最多加载 500 条事件 | 首次和“加载更多”均为 100 条；后端继续使用 `(timestamp,id)` 游标 |
| `management.html` | React/样式/图片全部内联，单个 HTML 约 4.6 MB | 集成 Manager Plus 使用小 HTML + hash 静态资源，资源一年 immutable cache，并保留单文件回退 |
| Quota 自动禁用 | 只持久处理 Codex 严格 429 与 xAI 免费额度 | 保留严格规则；其他提供商仅在 402/403/429 且响应含明确未来 quota reset 时持久禁用并自动恢复 |

## 已确认本身有边界，无需改核心逻辑

| 路径 | 现有边界 |
|---|---|
| `GET /logs` | 支持 `cursor/after/limit`，尾读不会扫描全部旧日志 |
| `GET /request-error-logs` | 只返回文件名、大小、修改时间，不返回日志正文 |
| `GET /usage-queue` | 按 `count` 从队列弹出，Manager 默认 batch 100 |
| `POST /monitoring/analytics` | 只计算 `include` 指定聚合；事件使用游标；SQLite 有索引、小时预聚合与 Dashboard rollup |
| `GET /dashboard/summary` | 走独立聚合接口，不返回原始请求记录 |
| `GET /plugins`、`GET /plugin-store` | 返回插件元数据，不读取插件资源正文；资源正文使用独立路径 |
| `GET /auth-files/models` | 单凭证、模型摘要字段，不返回凭证 JSON |
| 下载/导入/导出接口 | 用户显式触发的流式或文件传输，不参与页面首屏 |

## 兼容规则

- 不带新查询参数的旧客户端继续获得原响应结构；
- 新字段均为附加字段，原 `files` 数组保留；
- `detail` 只用于当前页；`summary` 保留 UI 必需状态和安全的 Codex claim 摘要；`snapshot` 不含请求桶、路径和 JWT claim；
- 5xx 与无明确恢复时间的错误继续由 CLIProxyAPI 内存 `Unavailable/NextRetryAfter/ModelStates` cooldown 处理，不写成持久人工禁用状态；
- CPAMP 只恢复自己持有的 cooldown，不会自动启用原本已由用户禁用的账号。
