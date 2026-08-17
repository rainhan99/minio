# MinIO 内置 Console IAM 管理与审计设计

- 日期：2026-08-17
- 状态：设计已逐节确认，等待书面规格审阅
- MinIO 基线：`7aac2a2c5`
- Console 基线：`github.com/minio/console v1.7.7-0.20250905210349-2017f33b26e1`
- 目标运行形态：单仓库、单 MinIO 二进制、单 systemd 服务、内置 Console

## 1. 背景

当前 MinIO 最终社区版通过 `github.com/minio/console/api` 将 Console 嵌入
MinIO 进程。一个 `minio` 二进制同时监听两个端口：

- `9000`：S3 API 与 MinIO Admin API；
- `9001`：Web Console。

最终 Console 仅保留 Bucket 和对象浏览能力，用户、组、策略及 Access Key 等 IAM
管理页面已经移除。同时，原 `github.com/minio/console` 仓库已经无法直接访问，但本项目
锁定的不可变 Go 模块版本仍可作为最终 Console 源码基线。

本项目不再跟踪已经停止维护的上游，而是在当前 MinIO 仓库中恢复并长期维护 Console
源码，在不改变现有单进程部署方式的前提下，恢复完整 IAM 管理能力，并为所有 IAM
变更增加可追溯、可保留、适用于多节点的数据库审计。

## 2. 目标

1. 将最终 Console 源码纳入当前 MinIO 仓库，构建不再依赖已消失的 Console 源码仓库。
2. 保持一个 `minio` 二进制和一个 `minio.service`，继续由同一进程提供 `9000` 与
   `9001`。
3. 在 `9001` Console 中提供以下内置 IAM 管理能力：
   - 本地用户管理；
   - 用户组及成员管理；
   - 策略管理；
   - 用户/组与策略绑定；
   - Service Account Access Key 的创建、查询、禁用、删除和分阶段轮换；
   - IAM 操作审计查询。
4. 仅允许 root 或具有完整等价 IAM 管理权限的管理员使用 IAM 管理面。
5. 以 MinIO IAM 为唯一真相源；PostgreSQL 只保存操作状态和脱敏审计事件。
6. 支持多个 MinIO 节点共享 PostgreSQL，并正确处理幂等、迁移、分区维护和故障恢复。
7. Secret Key 只展示一次，并且绝不进入数据库、日志、追踪或浏览器持久化存储。
8. 审计保留时间可配置，生产环境按月分区管理。

## 3. 非目标

首期明确不包含：

- LDAP、OIDC 或其他外部身份源的配置管理；
- 在 PostgreSQL 中复制用户、组、策略或 Access Key 作为第二份 IAM 状态；
- 管理 MinIO root Access Key/Secret Key；
- 独立部署第二个 Console 服务或第二个 systemd 单元；
- 生产环境使用 SQLite 或在首期支持 MySQL；
- 自动同步或合并已经停止维护的上游仓库；
- 将未完成或结果未知的操作按保留策略直接删除；
- 批量 IAM 导入导出等未明确要求的扩展功能。

## 4. 已选方案与取舍

### 4.1 采用：单仓库、嵌入式 Console、单运行二进制

Console 作为仓库内的独立 Go 模块维护，但生产只发布根目录构建出的 `minio` 二进制。
Console 的 API 包和前端静态资源在构建时嵌入 MinIO。

该方案保持当前端口、进程、systemd 和认证链路不变，同时让 MinIO 与 Console 的版本、
CI、发布和回滚保持原子一致。

### 4.2 未采用：两个仓库

两个仓库能保持历史边界，但需要跨仓库版本锁定、发布协调和源码可用性管理，不符合当前
自主维护和一次发布的目标。

### 4.3 未采用：独立 Console 进程

独立进程会引入第二个 systemd 服务、独立 TLS、跨进程认证与高可用部署问题。当前 MinIO
已经能在同一进程中可靠提供 `9001`，没有必要增加该复杂度。

## 5. 源码与仓库布局

目标目录结构：

```text
minio/
├── cmd/                         # MinIO Server 与嵌入入口
├── internal/
├── console/                     # Console 完整源码，独立 Go 模块
│   ├── api/                     # Console 后端 API
│   ├── cmd/console/             # 保留历史兼容入口，不作为生产发布物
│   ├── models/
│   ├── pkg/
│   ├── web-app/                 # React 前端
│   ├── go.mod
│   └── swagger.yml
├── docs/superpowers/specs/
└── go.mod
```

根模块继续保持 MinIO 原模块身份，Console 子模块也保留其原模块身份，以避免无业务价值的
全量 import path 改写。根 `go.mod` 使用本地替换：

```text
replace github.com/minio/console => ./console
```

因此模块身份仅用于 Go 包解析，源码实际始终来自当前仓库，不会下载已消失的 Console
仓库。仓库不需要配置 `upstream` remote，只保留自主维护的 `origin`。

### 5.1 源码恢复规则

1. 最终 Console 基线从当前 MinIO `go.mod` 锁定的不可变模块版本恢复。
2. 已移除 IAM 功能只从可验证的官方历史 Console 版本恢复，再按最终基线接口逐项移植。
3. 不以第三方 fork 作为权威源码；第三方实现只能作为交叉参考。
4. 保留原 `LICENSE`、`NOTICE`、`CREDITS` 和版权头。
5. 增加来源清单，记录模块版本、历史标签/提交、文件校验值及恢复说明。
6. 先建立“行为不变”的源码迁入基线，通过构建与 `9000/9001` 回归后再开发 IAM。

## 6. 运行架构

```text
Browser
  │ HTTPS :9001
  ▼
Embedded Console
  ├── Session/AuthN/AuthZ
  ├── IAM HTTP Handlers
  ├── IAM Application Service
  ├── Audit Coordinator ───────────────► Shared PostgreSQL
  └── madmin Adapter
          │ loopback/in-process endpoint :9000
          ▼
      MinIO Admin API
          ▼
      MinIO IAM（唯一真相源）
```

生产仍由同一个 MinIO 进程监听 `9000` 和 `9001`。浏览器地址始终停留在 `9001`；
Console 后端使用当前已验证会话派生的凭据调用 `9000` Admin API，浏览器不直接持有
madmin 凭据，也不应被重定向到 `9000`。

多个 MinIO 节点各自运行相同二进制，并连接同一 PostgreSQL。IAM 状态继续由 MinIO
集群维护；数据库仅协调 Console IAM 操作和保存审计。

## 7. 组件边界

### 7.1 Session 与权限组件

- 复用现有 Console 登录、会话和 STS 凭据链路；
- 只信任服务端验证后的会话主体、组和策略；
- 不接受浏览器传入的操作者、角色或权限声明；
- 为每个 IAM 动作输出明确的权限检查结果。

### 7.2 IAM Application Service

- 定义用户、组、策略、绑定和 Access Key 的业务用例；
- 统一执行输入校验、权限检查、幂等、审计编排和错误映射；
- 不直接依赖 GORM 或具体 madmin 客户端；
- 不缓存或复制 IAM 实体。

### 7.3 madmin Adapter

- 将应用用例映射到最终 MinIO/madmin 版本提供的真实 Admin API；
- 隔离 madmin 版本细节和错误类型；
- 保留 MinIO Admin API 的最终授权检查；
- 支持测试替身和真实契约测试。

### 7.4 Audit Coordinator

- 在 IAM 写入前持久化操作意图；
- 在调用 Admin API 后持久化结果事件；
- 对不确定结果建立可核对状态；
- 对 Secret、密码、会话令牌和凭据字段执行集中脱敏。

### 7.5 Repository

- 暴露小型、面向用例的操作状态和审计事件接口；
- GORM 用于常规查询、事务与连接管理；
- 版本化 SQL 负责生产迁移；
- PostgreSQL adapter 负责分区和 advisory lock；
- SQLite adapter 只用于单节点开发和快速单元测试。

### 7.6 Reconciler 与 Partition Maintainer

- Reconciler 核对超时 `pending` 和 `unknown` 操作；
- Partition Maintainer 创建未来分区并删除符合保留规则的完整旧分区；
- 两类任务都通过 PostgreSQL advisory lock 保证集群中只有一个有效执行者。

## 8. IAM API

Console 后端新增统一资源命名空间：

| 资源 | 主要接口 | 能力 |
|---|---|---|
| Users | `/api/v1/iam/users` | 列表、详情、创建、重置密码、启用、禁用、删除 |
| Groups | `/api/v1/iam/groups` | 列表、详情、创建、成员增删、启用、禁用、删除空组 |
| Policies | `/api/v1/iam/policies` | 列表、详情、创建、更新、删除、语法校验 |
| Bindings | `/api/v1/iam/policy-bindings` | 查询、绑定、解绑用户或组策略 |
| Access Keys | `/api/v1/iam/access-keys` | 列表、创建、禁用、删除、分阶段轮换 |
| Operations | `/api/v1/iam/operations/{operationID}` | 查询写操作及异常核对状态 |
| Audit | `/api/v1/iam/audit-events` | 分页、过滤、查看事件详情 |

路径和 Swagger 模型由同一份 `swagger.yml` 生成，前后端不得手写两套不一致的 DTO。

### 8.1 写接口公共协议

所有 IAM 写接口必须：

1. 验证 Console 会话；
2. 验证对应 Admin Action；
3. 验证 CSRF 与请求来源；
4. 接收 UUID 格式的 `Idempotency-Key`；
5. 对标准化后的请求计算 `request_hash`；
6. 在调用 madmin 前创建审计意图；
7. 返回 `operation_id`，使浏览器能够查询最终状态。

相同幂等键和相同请求返回原操作结果；相同幂等键对应不同请求时返回 `409 Conflict`。
Access Key 创建重试永远不会再次返回 Secret；若 Secret 已经签发，返回
`409 SECRET_ALREADY_ISSUED` 和操作 ID，管理员必须撤销该 Key 后重新创建。

### 8.2 统一错误

错误响应只包含稳定错误码、可展示信息、请求 ID 和可选操作 ID，不向浏览器泄漏数据库
DSN、SQL、Secret、内部堆栈或 madmin 原始敏感内容。

- `401`：未登录或会话失效；
- `403`：没有所需 Admin Action；
- `409`：幂等冲突、资源状态冲突或 Secret 已签发；
- `422`：输入或策略文档无效；
- `503`：审计数据库不可用，或执行结果暂时无法确认。

结果不确定时，`503` 响应必须携带 `operation_id` 和 `unknown` 状态，不能伪装成成功。

## 9. 权限模型

IAM 管理面入口只对以下主体开放：

1. MinIO root；或
2. 有效策略包含完整 IAM 管理 Action 集合的管理员。

不以策略名称字符串判断权限。名为 `consoleAdmin` 的策略如果被修改，不自动获得特权；
名称不同但具有等价 Action 的管理员可以使用管理面。进入管理面后，每个按钮和后端接口
仍按其精确 Action 二次检查，MinIO Admin API 再做最终授权。

主要映射如下：

| 功能 | MinIO Admin Action |
|---|---|
| 创建/重置密码/删除用户 | `admin:CreateUser` / `admin:DeleteUser` |
| 查询用户 | `admin:ListUsers` / `admin:GetUser` |
| 启用/禁用用户 | `admin:EnableUser` / `admin:DisableUser` |
| 查询组 | `admin:ListGroups` / `admin:GetGroup` |
| 管理组成员或删除空组 | `admin:AddUserToGroup` / `admin:RemoveUserFromGroup` |
| 启用/禁用组 | `admin:EnableGroup` / `admin:DisableGroup` |
| 创建/删除/查询策略 | `admin:CreatePolicy` / `admin:DeletePolicy` / `admin:GetPolicy` |
| 查询策略关联 | `admin:ListUserPolicies` |
| 绑定/解绑策略 | `admin:AttachUserOrGroupPolicy` / `admin:UpdatePolicyAssociation` |
| 创建 Access Key | `admin:CreateServiceAccount` |
| 更新或禁用 Access Key | `admin:UpdateServiceAccount` |
| 删除 Access Key | `admin:RemoveServiceAccount` |
| 查询 Access Key | `admin:ListServiceAccounts` |

审计查询没有对应的 MinIO Action，因此只允许已经通过完整 IAM 管理入口检查的管理员访问。

## 10. Secret 与传输安全

### 10.1 Secret 生命周期

- Access Key 与 Secret Key 由 MinIO Admin API 生成；
- Secret 仅存在于 madmin 响应、当前 HTTP 响应和前端一次性内存状态中；
- 数据库操作状态和审计事件只能保存 Access Key ID，不得保存 Secret；
- 日志、错误、指标标签、追踪 Span、Redux 持久化、Local Storage、Session Storage 和 URL
  均不得包含 Secret；
- Secret 响应设置 `Cache-Control: no-store`、`Pragma: no-cache` 和
  `Referrer-Policy: no-referrer`；
- 一次性弹窗关闭、路由切换或页面刷新后立即丢弃 Secret；
- 服务端没有“重新查看 Secret”接口。

本地用户密码同样禁止进入日志、数据库和审计 payload；它只用于当前创建或更新请求。

### 10.2 Access Key 轮换

轮换采用不中断业务的分阶段流程：

1. 为目标用户创建新的 Service Account Access Key；
2. 一次性展示新 Secret，由管理员更新客户端；
3. 管理员显式禁用旧 Key；
4. 观察完成后显式删除旧 Key。

首期不直接覆盖旧 Secret，也不在同一事务中自动删除旧 Key，避免客户端尚未切换时造成
业务中断。每一步都是独立、可审计、幂等的 IAM 操作。

### 10.3 HTTPS 与浏览器安全

- 生产 IAM 管理必须使用 HTTPS；
- MinIO 原生 TLS 或受信任反向代理均可终止 TLS；
- 只有来自明确配置的可信代理时才接受 `X-Forwarded-Proto` 和客户端 IP 头；
- 会话 Cookie 使用 `Secure`、`HttpOnly` 和适当的 `SameSite`；
- 写请求要求同源 Origin/Referer 校验和 CSRF Token；
- 密码、Secret 和策略写入接口限制请求大小并使用严格内容类型；
- 未启用安全传输时，IAM Secret/密码写操作保持禁用，并在页面显示明确配置错误。

## 11. 写操作与异常收敛

正常写入流程：

```text
验证会话和权限
  → 校验幂等键与请求摘要
  → DB 事务：创建 pending 操作 + intent 审计事件
  → 调用 madmin/Admin API
  → DB 事务：更新操作状态 + 写入 result 审计事件
  → 从 MinIO 重新读取资源
  → 返回结果与 operation_id
```

### 11.1 Fail-closed 规则

- 调用 Admin API 前数据库不可用：拒绝 IAM 写入，返回 `503`；
- 读取用户、组、策略和 Access Key 时不依赖审计数据库，数据库故障不会影响 S3 API 或
  IAM 只读查询；
- 审计查询在数据库不可用时单独返回不可用状态。

### 11.2 不确定结果

如果 Admin API 可能已经执行，但结果审计无法提交：

1. 不自动重复调用该 Admin API；
2. 向客户端返回操作 ID 和 `unknown`；
3. 数据库恢复后，Reconciler 扫描超时 `pending` 与 `unknown`；
4. Reconciler 从 MinIO 读取真实资源，与 `desired_state_hash` 比较；
5. 写入核对事件并转为明确终态。

Access Key 创建是特殊敏感操作。如果无法同时确认结果、完成审计并安全返回一次性 Secret，
Reconciler 必须优先禁用或删除新建 Key，记录 `compensated`，而不是让无法交付的凭据长期
保持有效。

### 11.3 状态机

操作状态包括：

- `pending`：意图已落库，Admin API 尚未得到可靠终态；
- `succeeded`：Admin API 和结果审计均成功；
- `failed`：确认未达到目标状态；
- `unknown`：调用结果不确定；
- `reconciled_succeeded`：核对后确认目标状态已经生效；
- `reconciled_failed`：核对后确认目标状态未生效；
- `compensated`：敏感或部分完成操作已撤销；
- `manual_review`：无法自动判断，必须由管理员处理。

只允许从 `pending`/`unknown` 进入核对终态。终态不可被普通请求重新打开；需要重试时必须
创建新的操作 ID。

## 12. 审计数据模型

### 12.1 `console_iam_operations`

该表不分区，是幂等与异常收敛的控制表。核心字段：

- `operation_id UUID PRIMARY KEY`；
- `idempotency_key UUID UNIQUE NOT NULL`；
- `request_hash`、`desired_state_hash`；
- `actor_principal`、`actor_auth_type`；
- `action`、`resource_type`、`resource_id`；
- `node_id`、`request_id`、`source_ip`、`user_agent`；
- `status`、`error_code`、脱敏后的 `error_message`；
- `safe_metadata JSONB`；
- `created_at`、`updated_at`、`completed_at`；
- `version`，用于乐观并发控制。

`safe_metadata` 采用按 Action 定义的允许字段列表，禁止将任意请求对象直接序列化入库。

### 12.2 `console_iam_audit_events`

该表是追加写事件表，以 `occurred_at` 按月做 PostgreSQL RANGE 分区。核心字段：

- `event_id UUID`；
- `occurred_at TIMESTAMPTZ`；
- `operation_id UUID`；
- `sequence`；
- `phase`：`intent`、`result`、`reconcile` 或 `compensation`；
- 操作者、Action、资源、节点和请求关联字段；
- `outcome`；
- `safe_payload JSONB`。

分区表主键包含分区键，例如 `(occurred_at, event_id)`；`operation_id`、操作者、Action、
资源和结果建立查询索引。

事件表禁止应用执行 UPDATE。除受控的整分区保留清理外，禁止逐行 DELETE。数据库权限只
授予 MinIO 专用审计 schema 所需的最小范围，不授予其他业务 schema 权限。

## 13. 分区与保留策略

- 默认保留：365 天；
- 配置项：`MINIO_CONSOLE_AUDIT_RETENTION_DAYS`；
- 分区粒度：自然月；
- 提前创建：当前月之后三个月；
- 创建与删除分区前获取固定 PostgreSQL advisory lock；
- 只删除上界已经完整早于保留截止时间的整个月分区；
- 如果候选分区关联任何 `pending`、`unknown` 或 `manual_review` 操作，则整分区暂缓删除；
- 控制表中已到期的终态操作在对应审计分区安全删除后再清理；
- 保留天数变更只影响后续维护任务，不进行同步大批量逐行删除。

SQLite 不实现物理分区，只按同样保留语义删除测试数据；它不能用于生产多节点部署。

## 14. 多节点协调与迁移

### 14.1 数据库连接

- 所有节点使用同一个 PostgreSQL DSN；
- DSN 只能来自安全配置，禁止打印完整值；
- 每个节点使用有界连接池；
- 数据库账号的权限限定在 Console 审计 schema；
- PostgreSQL 连接在生产环境启用 TLS。

### 14.2 Schema 迁移

- 迁移使用带版本号、可审查的 PostgreSQL SQL 文件；
- 生产禁止 GORM `AutoMigrate`；
- 节点启动时获取 migration advisory lock，只有持锁节点执行未完成迁移；
- 其他节点等待锁释放后验证 schema 版本；
- 迁移失败不会阻止 MinIO S3 服务启动，但 IAM 写管理进入只读保护并报告健康状态；
- 破坏性 schema 变更不自动执行，必须作为独立升级步骤审批。

### 14.3 后台任务

- 分区维护使用集群级 advisory lock；
- Reconciler 使用集群级选主锁和数据库行锁，避免两个节点处理同一操作；
- 节点崩溃后锁由 PostgreSQL 会话自动释放，其他节点可以接管；
- 幂等键在共享数据库中全局唯一，因此请求切换节点不会重复执行。

## 15. Console 页面与交互

```text
Console :9001
├── Object Browser
└── Identity & Access
    ├── Users
    ├── Groups
    ├── Policies
    ├── Access Keys
    └── Audit Logs
```

### 15.1 Users

- 搜索、状态过滤和分页；
- 创建本地用户；
- 重置本地用户密码，密码只存在于当前请求内；
- 查看组、绑定策略和 Access Key；
- 启用、禁用和删除；
- 删除等破坏性操作要求输入目标名称确认。

### 15.2 Groups

- 创建和查看组；
- 添加、移除成员；
- 查看和维护策略绑定；
- 启用、禁用组；
- 仅按最终 MinIO Admin API 支持的语义删除空组。

### 15.3 Policies

- 区分内置策略和自定义策略；
- 内置策略只读；
- 自定义策略支持 JSON 编辑、服务端解析校验、更新和删除；
- 删除由 MinIO 校验关联关系，UI 不绕过服务器约束。

### 15.4 Access Keys

- 列表只显示 Access Key ID、父用户、状态、创建时间、过期时间和策略摘要；
- 这里只管理 Service Account Access Key，不允许修改 root 凭据或本地用户的主凭据；
- 创建结果使用一次性 Secret 弹窗；
- 弹窗明确提示关闭后不可恢复；
- 支持复制，但不写入持久化前端状态；
- 禁用与删除需要确认；
- 轮换使用“创建新 Key、切换、禁用旧 Key、删除旧 Key”的引导流程。

### 15.5 Audit Logs

- 按时间、操作者、Action、资源、状态、节点和操作 ID 过滤；
- 展示一次操作的 intent、result、reconcile、compensation 时间线；
- 所有 payload 都是服务端脱敏后的允许字段；
- `pending`、`unknown` 和 `manual_review` 使用醒目标识并提供刷新入口。

所有写页面在响应成功后重新读取 MinIO 状态，不使用脱离真实 IAM 状态的乐观更新。

## 16. 配置与健康状态

首期配置：

| 配置 | 说明 |
|---|---|
| `MINIO_CONSOLE_AUDIT_DB_TYPE` | `postgres`；开发/测试可显式使用 `sqlite` |
| `MINIO_CONSOLE_AUDIT_DSN` | PostgreSQL DSN，属于敏感配置 |
| `MINIO_CONSOLE_AUDIT_RETENTION_DAYS` | 审计保留天数，默认 365 |
| `MINIO_CONSOLE_AUDIT_NODE_ID` | 可选稳定节点标识；默认从 MinIO 节点身份派生 |

配置读取、校验和日志输出集中在一个组件中。DSN、密码和 Token 使用统一敏感值包装，禁止
通过 `%v`、JSON 或配置转储泄漏。

Console 健康信息分别报告：

- MinIO IAM 是否可读；
- 审计数据库是否可用；
- schema 是否兼容；
- IAM 写入是否启用；
- Reconciler 与分区维护最近一次成功时间。

数据库异常只使 IAM 写入降级，不改变 MinIO 节点的 S3 就绪状态。

## 17. 构建与发布

构建顺序：

```text
console/web-app：Yarn 锁定依赖并构建 React
  → 生成/同步 Swagger TypeScript 客户端
  → 将静态资源嵌入 console Go 包
  → 测试 console 子模块
  → 根模块通过本地 replace 编译 minio
  → 生成唯一生产发布物 minio
```

CI 必须验证：

- Console 前端生成物与源码同步；
- Swagger 与 Go/TypeScript 模型同步；
- 根模块没有回退到网络下载 `github.com/minio/console`；
- `cmd/console` 历史入口至少保持可编译，但不生成正式部署产物；
- 最终二进制同时提供 `9000` 与 `9001`。

## 18. 测试策略

### 18.1 Go 单元测试

- Action 级权限映射；
- 输入校验和稳定错误映射；
- 幂等键、请求摘要和状态机；
- Secret/密码集中脱敏；
- Audit Coordinator 的正常和异常路径；
- Repository 使用 SQLite 的快速测试；
- Reconciler 决策和 Access Key 补偿。

### 18.2 PostgreSQL 集成测试

- 全量版本化迁移；
- 月分区创建和索引；
- 保留策略与受保护状态；
- advisory lock 竞争和故障释放；
- 多节点并发幂等；
- 事务回滚、连接中断和数据库恢复；
- GORM 查询与原生分区 SQL 的一致性。

SQLite 测试不能替代 PostgreSQL 集成测试。

### 18.3 MinIO Admin API 契约测试

- 使用当前最终版 MinIO 与锁定 madmin 版本；
- 覆盖用户、组、策略、绑定和 Service Account 的真实返回及错误；
- 验证 Action 权限与 root 行为；
- 验证写后重新读取的状态一致性。

### 18.4 前端与浏览器测试

- React/Jest：表单、权限门控、Secret 一次性状态、错误提示；
- Playwright：登录、用户、组、策略、绑定、Access Key、轮换和审计完整流程；
- 验证刷新、返回、浏览器存储和网络缓存都不能重新获得 Secret；
- 验证 URL 始终保持 `9001`，不会跳转到 `9000`；
- 回归现有 Bucket 浏览、上传、下载和对象操作。

### 18.5 故障注入与安全测试

- 意图审计前数据库断开；
- Admin API 超时或连接中断；
- Admin API 成功后结果审计失败；
- 相同幂等键跨节点重复提交；
- Access Key 响应丢失后的补偿；
- CSRF、伪造代理头、越权和请求大小限制；
- 对日志、数据库、浏览器存储和测试报告执行 Secret 模式扫描。

并发相关改动运行 race 测试；根仓库最终通过项目既有 verifier 和相关完整测试。

## 19. 迁入、上线与回滚

### 19.1 迁入顺序

1. 将最终 Console 源码和来源清单迁入 `console/`；
2. 使用本地 `replace` 完成行为不变构建；
3. 验证单二进制、`9000/9001`、登录和对象浏览回归；
4. 恢复历史 IAM 页面和处理器，并适配最终 madmin；
5. 引入 Audit Coordinator、GORM Repository、版本化迁移和 PostgreSQL；
6. 完成 UI、浏览器测试和多节点故障测试；
7. 在测试环境验证后部署到 `10.0.1.119`。

该顺序将“源码恢复问题”和“IAM 新功能问题”分开，任何阶段失败都能明确定位。

### 19.2 上线前置条件

- PostgreSQL 已建立专用数据库/schema、账号和 TLS 连接；
- 版本化迁移验证通过；
- MinIO Console 已配置 HTTPS；
- 审计保留天数已确认；
- 旧 MinIO 二进制和 systemd 配置已有可恢复备份；
- 全部密钥通过环境文件或安全配置注入，未进入仓库。

### 19.3 回滚

- 回滚只替换为上一版 `minio` 二进制并重启同一个 `minio.service`；
- 审计 schema 采用向前兼容的增量迁移，普通二进制回滚不自动执行 down migration；
- PostgreSQL 中已经写入的审计数据保留；
- 破坏性数据库回滚必须单独审批和备份，不能由应用启动自动执行；
- 回滚后 `9000` 与 `9001` 仍保持原端口职责。

## 20. 可观测性

日志只记录请求 ID、操作 ID、Action、资源类型、脱敏资源 ID、节点和稳定错误码。指标包括：

- IAM 写操作成功、失败、未知和补偿计数；
- 审计写入延迟与失败计数；
- `pending/unknown/manual_review` 数量与最老年龄；
- Reconciler 成功和失败计数；
- 分区维护最近成功时间；
- 数据库连接池状态。

Secret、密码、Cookie、Token、完整 DSN 和策略正文不得作为日志或指标标签。

## 21. 验收标准

1. 仓库包含可审计来源的完整 Console 源码，根构建不再下载 Console 源码模块。
2. 只发布一个 `minio` 二进制，只运行一个 `minio.service`。
3. `9000` 提供 S3/Admin API，`9001` 提供完整 Console，浏览器不跳转到 `9000`。
4. root 和等价 IAM 管理员能在页面管理用户、组、策略、绑定和 Access Key。
5. 普通用户及权限不完整的用户无法访问 IAM 管理面或绕过后端接口。
6. MinIO IAM 是所有页面读取和写后验证的唯一真相源。
7. IAM 写入在审计数据库不可用时被拒绝，但 S3 与 IAM 读取不受影响。
8. Secret Key 只出现一次，数据库、日志、追踪、缓存和浏览器持久化中均不存在 Secret。
9. 多节点重复提交只产生一次 IAM 变更，异常结果能够核对或补偿。
10. 审计按月分区、默认保留 365 天，并保护所有未收敛操作。
11. PostgreSQL 迁移、并发、分区、故障注入、Go、React 和 Playwright 测试通过。
12. 现有 Bucket 和文件浏览上传功能无回归。

## 22. 后续实施边界

本设计批准后，实施计划必须拆成可独立验证的阶段，至少包含源码迁入基线、IAM 后端、审计
存储、异常协调、前端页面、端到端验证和部署。每个阶段先写失败测试，再实现最小完整功能，
并在进入下一阶段前验证现有 `9000/9001` 行为没有漂移。

任何 Git 提交/推送、生产数据库 schema 变更、systemd 修改或部署操作，均须按项目规则在
执行前取得明确确认。
