# MinIO 内置 Console IAM 管理与审计实施计划

> **For Codex:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task.

**目标：** 在当前 MinIO 仓库内恢复并自主维护最终版 Console 源码，保持单个 `minio` 二进制和单个 systemd 服务，在 `9001` 提供完整 IAM 管理、一次性 Secret 交付、多节点 PostgreSQL 审计、异常核对与按月分区保留能力。

**架构：** `console/` 保持 `github.com/minio/console` 独立 Go 模块身份，由根模块通过本地 `replace` 嵌入。Console 后端采用应用服务、madmin adapter、审计协调器和 repository ports 分层；MinIO IAM 是实体状态唯一真相源，PostgreSQL 只保存幂等操作控制状态与脱敏审计事件。生产仍仅发布根目录构建的 `minio`，同一进程监听 `9000` 和 `9001`。

**技术栈：** 最低 Go 1.25.13、生产 Go 1.26.6、madmin-go/v3、GORM、PostgreSQL、SQLite（仅带 `iam_sqlite` 构建标签的开发/单元测试）、React 18、TypeScript、Redux Toolkit、Swagger/OpenAPI、Jest、Playwright、systemd user service。

**设计依据：** `docs/superpowers/specs/2026-08-17-minio-console-iam-management-design.md`

---

## 执行约束

1. 本计划依赖 [Go 工具链兼容升级 Task 0](2026-08-17-go-toolchain-compatibility.md)。Task 0 未通过源码、双版本和独立干净检出发布物门禁前，Task 1 保持阻塞。
2. 开始实施前先使用 `superpowers:using-git-worktrees` 建立隔离工作区，再使用 `superpowers:test-driven-development`；每个任务严格执行红灯、最小实现、绿灯、重构。
3. 不手工编辑 Swagger、静态资源或其他生成文件；修改生成源后运行仓库既有生成命令，并提交源文件与生成结果。
4. 不引入独立生产 Console 二进制、第二个 systemd 单元或浏览器直连 `9000` 的 madmin 凭据。
5. `console_iam_operations` 和 `console_iam_audit_events` 不保存 IAM 实体副本。IAM 列表、详情和写后确认均从 MinIO 读取。
6. Secret、密码、Cookie、Token、完整 DSN 和策略正文不得进入数据库、日志、追踪、指标标签、URL、Redux 持久化或浏览器存储。
7. 生产仅支持共享 PostgreSQL。SQLite adapter 只在 `iam_sqlite` 构建标签下编译，避免生产 `CGO_ENABLED=0` 构建链接 SQLite 驱动。
8. 生产迁移只运行版本化 SQL；禁止 GORM `AutoMigrate`。
9. 每个任务的提交命令只是建议检查点。执行 `git commit` 前必须再次取得用户明确确认；不得擅自 `git push`。
10. 数据库 schema 变更、systemd 配置修改和向 `rain@10.0.1.119` 部署前必须按仓库危险操作格式单独确认。
11. 计划中的路径是目标路径。若迁入的最终 Console 源码已有同职责文件，优先扩展原文件并保持其命名约定，禁止为了匹配计划重复实现。

## 锁定基线与来源

- MinIO 设计基线：`7aac2a2c5`。
- 最终 Console 模块：`github.com/minio/console v1.7.7-0.20250905210349-2017f33b26e1`。
- 最终 Console `go.sum`：`h1:jOW1ggtITn8sreTzUjcdYE/ZffxeVmWstXNlBLOE6j4=`。
- IAM 历史移植参考：官方 `github.com/minio/console v1.7.6`。
- v1.7.6 `go.sum`：`h1:E0jq9nYMeW7z4iJtJ6vDt2hk4Jin0zcyAzRcTlaUO44=`。
- 根模块当前选择：`github.com/minio/madmin-go/v3 v3.0.109`；实现必须按该版本真实方法签名适配。
- 最终模块是主要源码，v1.7.6 只提供 IAM handler、Swagger 和页面行为参考；不得恢复 IDP、监控、KMS、事件目标或设置中心。

## 核心类型契约

以下契约在任务 4 建立，后续任务不得各自定义平行状态或 repository：

```go
type OperationStatus string

const (
	OperationPending             OperationStatus = "pending"
	OperationSucceeded           OperationStatus = "succeeded"
	OperationFailed              OperationStatus = "failed"
	OperationUnknown             OperationStatus = "unknown"
	OperationReconciledSucceeded OperationStatus = "reconciled_succeeded"
	OperationReconciledFailed    OperationStatus = "reconciled_failed"
	OperationCompensated         OperationStatus = "compensated"
	OperationManualReview        OperationStatus = "manual_review"
)

type Repository interface {
	CreateIntent(context.Context, Operation, AuditEvent) (Operation, bool, error)
	Complete(context.Context, uuid.UUID, uint64, OperationStatus, AuditEvent) error
	GetOperation(context.Context, uuid.UUID) (Operation, error)
	ListUnsettled(context.Context, ReconcileFilter) ([]Operation, error)
	ListAuditEvents(context.Context, AuditFilter) (AuditPage, error)
}

type BackendError struct {
	Code         string
	OutcomeKnown bool
	Err          error
}

type MutationFunc func(context.Context) (MutationResult, error)

type Coordinator interface {
	Execute(context.Context, Intent, MutationFunc) (ExecutionResult, error)
}
```

允许的普通终态只有 `succeeded` 和 `failed`；只允许从 `pending` 或 `unknown` 进入 `reconciled_succeeded`、`reconciled_failed`、`compensated` 或 `manual_review`。乐观锁版本冲突不得覆盖其他节点已经写入的终态。

## Task 1：迁入最终 Console 源码并记录来源

**Files:**

- Create: `console/**`（从锁定模块完整恢复）
- Create: `console/UPSTREAM-SOURCE.md`
- Modify: `go.mod`
- Modify: `go.sum`
- Test: `buildscripts/verify-console-local-module.sh`

**Step 1：写失败的本地模块验证脚本**

创建 `buildscripts/verify-console-local-module.sh`，验证：

- `console/go.mod` 的 module 必须为 `github.com/minio/console`；
- `console/go.mod` 的 `go` 指令必须为 `1.25.0`；
- `console/go.mod` 的 `toolchain` 指令必须为 `go1.26.6`；
- 根 `go.mod` 必须包含 `replace github.com/minio/console => ./console`；
- `go list -m -json github.com/minio/console` 的 `Dir` 必须位于仓库的 `console/`；
- `console/LICENSE`、`console/NOTICE`、`console/CREDITS` 与来源说明存在；
- 禁止把模块缓存路径或临时目录写入源码。

Run: `bash "buildscripts/verify-console-local-module.sh"`

Expected: FAIL，报告 `console/go.mod` 或本地 `replace` 缺失。

**Step 2：危险操作检查点**

迁入完整模块属于批量文件写入。执行前输出仓库规定的危险操作确认，说明目标仅为 `console/`、`go.mod`、`go.sum` 和验证脚本；取得明确确认后再继续。

**Step 3：恢复不可变最终模块**

从 Go module cache 中锁定的最终版本复制完整源码到 `console/`，保留许可文件、生成器、前端锁文件和 Git 属性。删除模块缓存只读权限等传输属性，但不改业务代码。不得从不明第三方 fork 合并文件。

`console/UPSTREAM-SOURCE.md` 必须记录：模块版本、伪版本对应提交、模块校验值、恢复日期、历史 IAM 参考版本及其校验值、未恢复功能清单。

源码恢复后只对齐模块工具链指令，不在复制步骤升级依赖：

```bash
(cd "console" && GOTOOLCHAIN=go1.26.6 go mod edit -go=1.25.0 -toolchain=go1.26.6)
(cd "console" && GOTOOLCHAIN=go1.26.6 go mod tidy -compat=1.21 -diff)
```

`tidy -diff` 若显示依赖变化，先为实际 Go 1.26 不兼容建立失败测试并单独评估，禁止把依赖升级混入源码迁入。

**Step 4：绑定根模块到本地 Console**

在根 `go.mod` 增加：

```text
replace github.com/minio/console => ./console
```

先运行 `GOTOOLCHAIN=go1.26.6 go mod tidy -compat=1.21 -diff` 审核差异，再执行 tidy；仅接受由本地模块切换产生的必要 `go.sum` 变化。

**Step 5：验证模块来源和子模块基线**

Run: `bash "buildscripts/verify-console-local-module.sh"`

Expected: PASS，`go list` 显示本地 `console/`。

Run: `(cd "console" && GOTOOLCHAIN=go1.25.13 go test ./api/... ./pkg/... ./models/...)`

Run: `(cd "console" && GOTOOLCHAIN=go1.26.6 go test ./api/... ./pkg/... ./models/...)`

Expected: 两个受支持工具链均 PASS，最终 Console 迁入后行为不变。

**Step 6：建议提交检查点**

```bash
git add "console" "go.mod" "go.sum" "buildscripts/verify-console-local-module.sh"
git commit -m "build: vendor final Console source"
```

## Task 2：建立单二进制和双端口回归基线

**Files:**

- Modify: `Makefile`
- Modify: `buildscripts/verify-console-local-module.sh`
- Create: `docs/console-development.md`
- Test: `cmd/common-main_test.go`

**Step 1：写失败的嵌入配置回归测试**

在 `cmd/common-main_test.go` 增加表驱动测试，断言 `initConsoleServer` 使用嵌入式 `console/api`，Console 地址与 S3 地址分别传递，且不创建第二个生产服务入口。将可测试的配置构造提取成无副作用函数，不在单元测试启动真实监听端口。

Run: `go test -tags kqueue,dev ./cmd -run 'TestConsoleServerConfig'`

Expected: FAIL，缺少可测试配置函数或断言未满足。

**Step 2：增加最小构建边界**

保持 `cmd/common-main.go` 中现有嵌入方式，只提取配置构造。更新 `Makefile`，让根构建先验证 `console/web-app` 生成物，再编译唯一生产产物 `./minio`。`console/cmd/console` 只做兼容编译检查，不进入发布目标。

**Step 3：记录本地开发命令和端口契约**

在 `docs/console-development.md` 记录：

```bash
./minio server "/tmp/minio-console-dev" --address :9000 --console-address :9001
```

并明确 `9000` 为 S3/Admin API，`9001` 为浏览器 Console，同一 PID 提供两个端口。

**Step 4：验证编译与端口烟测**

Run: `go test -tags kqueue,dev ./cmd -run 'TestConsoleServerConfig'`

Expected: PASS。

Run: `go build -o "/private/tmp/minio-console-baseline" .`

Expected: PASS，只生成一个根 MinIO 二进制。

在临时数据目录启动该二进制后验证：

- `curl -I http://127.0.0.1:9001/` 返回 Console 页面；
- `curl -i http://127.0.0.1:9000/` 返回 S3 XML 响应；
- 浏览器 Console 地址没有跳转到 `9000`。

**Step 5：建议提交检查点**

```bash
git add "Makefile" "buildscripts/verify-console-local-module.sh" "docs/console-development.md" "cmd/common-main.go" "cmd/common-main_test.go"
git commit -m "test: lock embedded Console runtime shape"
```

## Task 3：恢复 IAM OpenAPI 契约与生成链

**Files:**

- Modify: `console/swagger.yml`
- Modify: `console/api/configure_console.go`
- Modify: `console/api/generated.go`
- Modify: `console/models/**`
- Modify: `console/web-app/src/api/**`
- Test: `console/api/iam_contract_test.go`
- Test: `console/web-app/src/api/iamApi.spec.ts`

**Step 1：写失败的契约测试**

`console/api/iam_contract_test.go` 解析 `swagger.yml` 并断言存在以下前缀与操作：

- `/api/v1/iam/users`：list/get/create/set-password/set-status/delete；
- `/api/v1/iam/groups`：list/get/create/update-members/set-status/delete-empty；
- `/api/v1/iam/policies`：list/get/create/update/delete/validate；
- `/api/v1/iam/policy-bindings`：list/attach/detach；
- `/api/v1/iam/access-keys`：list/create/update-status/delete；
- `/api/v1/iam/operations/{operationID}`；
- `/api/v1/iam/audit-events`。

测试还要断言全部写操作声明 `Idempotency-Key`、CSRF header、稳定错误模型和 `operation_id`，Secret 只出现在 Access Key 创建成功模型。

Run: `(cd "console" && go test ./api -run 'TestIAMOpenAPIContract')`

Expected: FAIL，IAM paths 尚不存在。

**Step 2：从 v1.7.6 移植契约，不移植无关管理面**

以最终 `swagger.yml` 为主文件，选择性移植 v1.7.6 的用户、组、策略和 Service Account schema，统一到 `/api/v1/iam`。补充操作、审计、幂等和安全错误模型。不要移植 IDP、KMS、监控、事件目标和设置接口。

**Step 3：运行现有生成器**

按 `console/Makefile` 和生成注释运行 Swagger Go server/model 与 TypeScript client 生成命令。若生成器版本不再可复现，先锁定现有工具版本并修复生成目标，不手写 `generated.go` 或客户端 DTO。

**Step 4：注册空实现返回稳定的未实现错误**

在 `console/api/configure_console.go` 注册生成的 IAM route。handler 初始只调用共享错误映射返回 `501 IAM_NOT_READY`，以便契约和路由测试先稳定；后续任务逐资源替换。

**Step 5：验证契约与生成文件同步**

Run: `(cd "console" && go test ./api -run 'TestIAMOpenAPIContract')`

Expected: PASS。

Run: `(cd "console/web-app" && yarn test iamApi.spec.ts --runInBand)`

Expected: PASS，TypeScript 客户端与 OpenAPI 字段一致。

Run: `git diff --exit-code -- "console/api/generated.go" "console/models" "console/web-app/src/api"`

Expected: PASS after rerunning generators，生成物无漂移。

**Step 6：建议提交检查点**

```bash
git add "console/swagger.yml" "console/api/configure_console.go" "console/api/generated.go" "console/models" "console/web-app/src/api" "console/api/iam_contract_test.go"
git commit -m "api: define Console IAM contract"
```

## Task 4：建立 IAM 领域模型、状态机与端口

**Files:**

- Create: `console/internal/iam/domain.go`
- Create: `console/internal/iam/ports.go`
- Create: `console/internal/iam/errors.go`
- Create: `console/internal/iam/safefields.go`
- Test: `console/internal/iam/domain_test.go`
- Test: `console/internal/iam/safefields_test.go`

**Step 1：先写状态机失败测试**

覆盖每个允许和拒绝的状态转换，至少包括：

- `pending -> succeeded|failed|unknown`；
- `pending|unknown -> reconciled_succeeded|reconciled_failed|compensated|manual_review`；
- 所有终态不能被普通请求重新打开；
- 乐观锁版本每次成功更新加一。

Run: `(cd "console" && go test ./internal/iam -run 'TestOperationTransition')`

Expected: FAIL，领域类型尚未定义。

**Step 2：写安全字段和 Secret 失败测试**

为每个 Action 定义允许写入 `safe_metadata`/`safe_payload` 的字段集合。测试向输入注入 `secretKey`、`password`、`token`、`cookie`、`authorization`、`dsn` 和策略正文，确认全部被拒绝或固定脱敏。

定义 `SecretMaterial`：不得实现 `Stringer` 或 `json.Marshaler`，只允许 `Consume()` 一次，消费或丢弃后清零底层字节。测试第二次消费返回稳定错误。

Run: `(cd "console" && go test ./internal/iam -run 'TestSafeFields|TestSecretMaterial')`

Expected: FAIL。

**Step 3：实现最小领域层**

实现本计划“核心类型契约”中的状态、repository、coordinator、backend error 以及：

```go
type IAMBackend interface {
	Users(context.Context, UserFilter) ([]User, error)
	Groups(context.Context, GroupFilter) ([]Group, error)
	Policies(context.Context, PolicyFilter) ([]Policy, error)
	AccessKeys(context.Context, AccessKeyFilter) ([]AccessKey, error)
	Mutate(context.Context, BackendMutation) (MutationResult, error)
	Observe(context.Context, DesiredState) (Observation, error)
}
```

若实际 madmin 语义要求拆分，用 `UserReader`、`UserWriter` 等小接口替代单个接口，并只在需要多种能力的 application service 中组合；不得让 HTTP handler 直接依赖 `madmin.AdminClient` 或 `gorm.DB`。

**Step 4：验证领域不变量**

Run: `(cd "console" && go test ./internal/iam)`

Expected: PASS。

Run: `(cd "console" && go vet ./internal/iam/...)`

Expected: PASS。

**Step 5：建议提交检查点**

```bash
git add "console/internal/iam"
git commit -m "feat(console): add IAM domain contracts"
```

## Task 5：实现 GORM repository 与 SQLite 测试 adapter

**Files:**

- Create: `console/internal/iam/repository/models.go`
- Create: `console/internal/iam/repository/gorm_repository.go`
- Create: `console/internal/iam/repository/sqlite.go`
- Create: `console/internal/iam/repository/sqlite_disabled.go`
- Test: `console/internal/iam/repository/gorm_repository_test.go`

**Step 1：写 repository 契约失败测试**

使用带 `iam_sqlite` 构建标签的内存 SQLite 测试同一个 `Repository` 契约：

- `CreateIntent` 原子写入 operation 与 intent event；
- 同一幂等键和相同 `request_hash` 返回原 operation，`created=false`；
- 同一幂等键和不同摘要返回 `ErrIdempotencyConflict`；
- `Complete` 用 `operation_id + version` 乐观锁更新并追加结果事件；
- 事务中任一步失败必须全部回滚；
- `ListUnsettled` 只返回过期 `pending` 和 `unknown`；
- `ListAuditEvents` 使用稳定的 `(occurred_at,event_id)` 游标排序。

Run: `(cd "console" && go test -tags iam_sqlite ./internal/iam/repository -run 'TestRepositoryContract')`

Expected: FAIL，repository 尚未实现。

**Step 2：定义持久化模型而不复制 IAM 实体**

`models.go` 只定义 `OperationRecord` 与 `AuditEventRecord`。字段与设计文档第 12 节一致；敏感 payload 必须先通过 `iam.SafeFields`，repository 不接受任意请求对象。

GORM model 不执行 `AutoMigrate`。表名固定为：

- `minio_console.console_iam_operations`；
- `minio_console.console_iam_audit_events`。

SQLite 测试通过显式 fixture SQL 创建无 schema 前缀的同构表。

**Step 3：实现事务与乐观锁**

`gorm_repository.go` 只负责 SQL/GORM 映射、事务、分页和唯一冲突翻译。状态转换仍调用领域层，不能在 repository 中复制状态机。

`sqlite.go` 包含驱动导入并声明 `//go:build iam_sqlite`；`sqlite_disabled.go` 在未带标签时返回 `ErrSQLiteDisabled`，确保普通生产构建不链接 CGO driver。

**Step 4：验证快速 adapter 和生产编译边界**

Run: `(cd "console" && go test -tags iam_sqlite ./internal/iam/repository)`

Expected: PASS。

Run: `(cd "console" && CGO_ENABLED=0 go test ./internal/iam/repository)`

Expected: PASS，未链接 SQLite CGO 实现。

**Step 5：建议提交检查点**

```bash
git add "console/internal/iam/repository" "console/go.mod" "console/go.sum"
git commit -m "feat(console): add IAM audit repository"
```

## Task 6：增加 PostgreSQL 版本化迁移、分区和集群锁

**Files:**

- Create: `console/internal/iam/migrations/postgres/0001_console_iam.sql`
- Create: `console/internal/iam/migrations/postgres/0002_audit_partitions.sql`
- Create: `console/internal/iam/migrations/embed.go`
- Create: `console/internal/iam/repository/postgres.go`
- Create: `console/internal/iam/repository/migrator.go`
- Create: `console/internal/iam/repository/partitions.go`
- Test: `console/internal/iam/repository/postgres_integration_test.go`
- Test: `console/internal/iam/repository/partitions_integration_test.go`

**Step 1：写 PostgreSQL 集成失败测试**

测试从 `MINIO_CONSOLE_TEST_POSTGRES_DSN` 获取隔离数据库连接；未设置时只在普通本地测试跳过，CI PostgreSQL job 必须设置并执行。测试覆盖：

- 两个 migrator 并发启动时只有一个持有 migration advisory lock；
- migration 可重复执行且 schema 版本一致；
- 操作表幂等键全局唯一；
- 审计事件按 `occurred_at` 路由到月分区；
- 当前月和未来三个月分区存在；
- 两个 maintainer 竞争时只有一个执行 DDL；
- PostgreSQL 连接中断后 advisory lock 自动释放。

Run: `(cd "console" && go test -tags integration ./internal/iam/repository -run 'TestPostgresMigration|TestPartition')`

Expected: FAIL，迁移和 PostgreSQL adapter 尚未实现。

**Step 2：实现首版不可破坏迁移**

`0001_console_iam.sql` 创建：

```sql
CREATE SCHEMA IF NOT EXISTS minio_console;
CREATE TABLE minio_console.schema_migrations (...);
CREATE TABLE minio_console.console_iam_operations (...);
CREATE TABLE minio_console.console_iam_audit_events (...) PARTITION BY RANGE (occurred_at);
```

`console_iam_operations` 是不分区控制表，包含 operation/idempotency/request/desired hashes、actor、action、resource、node/request/source 字段、状态、安全 JSON、时间和 version。`console_iam_audit_events` 主键为 `(occurred_at,event_id)`，包含 operation、sequence、phase、主体、动作、资源、节点、结果和安全 JSON。

`0002_audit_partitions.sql` 只建立分区维护所需函数、索引或约束；不得包含会删除既有数据的 down migration。

**Step 3：实现迁移锁与 schema 兼容检查**

使用固定、文档化的 PostgreSQL advisory lock key。持锁连接必须是专用 `sql.Conn`，避免 GORM 连接池切换导致锁作用域错误。迁移失败返回独立健康状态，不阻止 MinIO S3 进程启动，但禁用 IAM 写入。

**Step 4：实现分区维护与保留保护**

`partitions.go`：

- 创建当前月及未来三个月分区；
- 分区边界使用 UTC 自然月；
- 只 drop 上界完整早于保留截止时间的分区；
- drop 前检查该月事件关联的 operation 是否存在 `pending`、`unknown` 或 `manual_review`；
- 任一受保护 operation 存在时暂缓整个分区；
- 安全 drop 后再批量清理已到期终态 operation；
- 使用参数化标识符白名单和数据库返回的 catalog 信息构造 DDL，禁止拼接用户输入。

**Step 5：验证迁移和多节点锁**

Run: `(cd "console" && go test -tags integration ./internal/iam/repository -run 'TestPostgresMigration|TestPartition|TestAdvisoryLock' -count=1)`

Expected: PASS against PostgreSQL。

Run: `(cd "console" && go test -race -tags integration ./internal/iam/repository -run 'TestConcurrentIdempotency' -count=1)`

Expected: PASS，只产生一条 IAM 操作意图。

**Step 6：建议提交检查点**

```bash
git add "console/internal/iam/migrations" "console/internal/iam/repository"
git commit -m "feat(console): add PostgreSQL IAM audit schema"
```

## Task 7：实现审计协调器、幂等和集中脱敏

**Files:**

- Create: `console/internal/iam/coordinator.go`
- Create: `console/internal/iam/hash.go`
- Create: `console/internal/iam/redactor.go`
- Create: `console/internal/iam/action_metadata.go`
- Test: `console/internal/iam/coordinator_test.go`
- Test: `console/internal/iam/redactor_test.go`

**Step 1：写协调流程失败测试**

用 fake repository 和 fake mutation 覆盖完整错误矩阵：

| 场景 | mutation 调用次数 | 最终结果 |
|---|---:|---|
| intent DB 写失败 | 0 | `503 AUDIT_UNAVAILABLE` |
| Admin API 明确成功，result 审计成功 | 1 | `succeeded` |
| Admin API 明确拒绝，result 审计成功 | 1 | `failed` |
| Admin API 超时且结果未知 | 1 | `unknown` + operation ID |
| Admin API 成功后 result DB 写失败 | 1 | `unknown`，禁止自动重放 |
| 相同 key/相同 hash 重试 | 0 | 返回原 operation |
| 相同 key/不同 hash 重试 | 0 | `409 IDEMPOTENCY_CONFLICT` |

Run: `(cd "console" && go test ./internal/iam -run 'TestCoordinator')`

Expected: FAIL。

**Step 2：实现标准化摘要**

为每种 Action 定义明确 request DTO 到 canonical JSON 的转换：稳定字段顺序、规范化集合顺序、排除 Secret/密码原文，并对敏感值只保留不可逆 HMAC 摘要。`request_hash` 用于幂等内容比较；`desired_state_hash` 用于 reconciler 观察结果比较。

**Step 3：实现先意图后变更的协调器**

`Coordinator.Execute` 必须依次：校验 intent、安全字段、创建 pending 与 intent event、调用一次 mutation、分类 `BackendError.OutcomeKnown`、完成 operation 与 result event。任何可能已执行的调用都不得在同一请求内自动重试。

**Step 4：实现统一脱敏器**

脱敏器处理结构化字段和 error chain；日志适配器只能接收脱敏后的 `SafeError`。测试用高熵 Secret、MinIO Access Key 形态、Bearer token、DSN 密码和 Cookie 验证输出中不存在原值。

**Step 5：运行故障矩阵与 race 测试**

Run: `(cd "console" && go test -race ./internal/iam -run 'TestCoordinator|TestRedactor|TestCanonicalHash')`

Expected: PASS。

**Step 6：建议提交检查点**

```bash
git add "console/internal/iam"
git commit -m "feat(console): coordinate audited IAM mutations"
```

## Task 8：强化 Console 会话授权、CSRF、HTTPS 和可信代理

**Files:**

- Modify: `console/api/user_session.go`
- Create: `console/api/iam_authorization.go`
- Create: `console/api/csrf.go`
- Create: `console/api/trusted_proxy.go`
- Create: `console/api/secure_transport.go`
- Create: `console/api/iam_runtime.go`
- Test: `console/api/iam_authorization_test.go`
- Test: `console/api/csrf_test.go`
- Test: `console/api/trusted_proxy_test.go`
- Test: `console/api/secure_transport_test.go`

**Step 1：写 Action 权限失败测试**

基于现有 `AccountInfo` 和 session permissions，表驱动验证：

- root 允许全部 IAM 管理动作；
- 策略名称为 `consoleAdmin` 但缺 Action 时拒绝；
- 策略名称不同但包含完整 Action 集时允许进入管理面；
- 进入后每个 handler 仍校验其精确 Action；
- 审计查询只允许通过完整 IAM 入口门槛的主体。

Action 常量必须使用 `github.com/minio/pkg/v3/policy`，禁止字符串散落在 handler。

Run: `(cd "console" && go test ./api -run 'TestIAMAuthorization')`

Expected: FAIL。

**Step 2：写 CSRF 与可信代理失败测试**

覆盖：

- CSRF token 由现有 cluster-stable `auth.derivedKey()` 和 session 标识生成 HMAC；
- 写请求同时要求 header token 与同源 Origin/Referer；
- token 恒定时间比较、过期 session 拒绝；
- 非可信来源的 `X-Forwarded-Proto`、`X-Forwarded-For`、`Forwarded` 全部忽略；
- 仅配置 CIDR 内代理可覆盖协议和客户端 IP；
- 多级代理链只剥离可信右侧 hop，不接受客户端伪造左侧值。

Run: `(cd "console" && go test ./api -run 'TestCSRF|TestTrustedProxy')`

Expected: FAIL。

**Step 3：实现 IAM runtime 依赖注入**

`iam_runtime.go` 统一持有 application services、repository 健康状态、authorization、CSRF 和 transport policy。通过 `api.Config` 或明确构造参数注入；不得使用可被测试并发污染的包级可变单例。

**Step 4：实现安全传输门控**

Secret/密码写 handler 只有在请求直接 TLS 或来自可信代理且 `X-Forwarded-Proto=https` 时启用。其他 IAM 读取仍可用，响应稳定返回 `503 IAM_SECURE_TRANSPORT_REQUIRED`。Cookie 继续使用 `HttpOnly`、`SameSite=Lax`，安全传输时必须 `Secure`。

**Step 5：实现统一响应安全头和限制**

一次性 Secret 响应设置：

```text
Cache-Control: no-store
Pragma: no-cache
Referrer-Policy: no-referrer
```

IAM 写请求要求 `application/json` 并用 `http.MaxBytesReader` 设置按资源定义的上限；策略文档允许较大但有界的尺寸，密码/Access Key 请求使用更小上限。

**Step 6：验证会话与安全边界**

Run: `(cd "console" && go test -race ./api -run 'TestIAMAuthorization|TestCSRF|TestTrustedProxy|TestSecureTransport')`

Expected: PASS。

**Step 7：建议提交检查点**

```bash
git add "console/api"
git commit -m "feat(console): secure IAM administration routes"
```

## Task 9：适配最终 madmin v3.0.109

**Files:**

- Create: `console/internal/iam/madminbackend/client.go`
- Create: `console/internal/iam/madminbackend/users.go`
- Create: `console/internal/iam/madminbackend/groups.go`
- Create: `console/internal/iam/madminbackend/policies.go`
- Create: `console/internal/iam/madminbackend/access_keys.go`
- Create: `console/internal/iam/madminbackend/errors.go`
- Test: `console/internal/iam/madminbackend/*_test.go`
- Test: `console/internal/iam/madminbackend/contract_integration_test.go`

**Step 1：写 adapter 失败测试**

定义最小 `AdminClient` 接口，由测试 fake 实现；生产 wrapper 调用 madmin v3.0.109 的真实方法：

- Users：`SetUser`/`SetUserReq`、`RemoveUser`、`ListUsers`、`GetUserInfo`、`SetUserStatus`；
- Groups：`UpdateGroupMembers`、`GetGroupDescription`、`ListGroups`、`SetGroupStatus`；
- Policies：`ListCannedPolicies`、`InfoCannedPolicyV2`、`AddCannedPolicy`、`RemoveCannedPolicy`、`GetPolicyEntities`、`AttachPolicy`、`DetachPolicy`；
- Access Keys：`AddServiceAccount`、`ListServiceAccounts`、`InfoServiceAccount`、`UpdateServiceAccount`、`DeleteServiceAccount`。

测试每个领域调用映射到准确方法与参数，并验证 madmin 原始响应对象不会被直接 JSON 返回给浏览器。

Run: `(cd "console" && go test ./internal/iam/madminbackend)`

Expected: FAIL，adapter 尚未实现。

**Step 2：实现会话派生客户端 factory**

复用现有 Console session 中经服务端验证的 STS 凭据和 MinIO endpoint 构造 madmin client。factory 每个请求产生作用域明确的 client，不缓存浏览器凭据，不把 Access Key/Session Token 写入日志。

**Step 3：实现错误分类**

`errors.go` 将明确的 MinIO 拒绝、资源不存在和状态冲突映射为 `BackendError{OutcomeKnown:true}`；网络超时、EOF、连接重置和无法解析的 5xx 映射为 `OutcomeKnown:false`。保留稳定代码和脱敏消息，原始 error 只在内存 error chain 中供内部判断。

**Step 4：实现领域观察读取**

每类 adapter 提供写后读取和 reconciler 所需的 `Observe`，比较规范化目标状态而非显示文案。策略比较基于规范化 JSON；组成员和绑定集合排序后比较；Access Key 只比较 ID、父主体、状态、过期时间和策略摘要，绝不读取或合成 Secret。

**Step 5：运行 fake 与真实契约测试**

Run: `(cd "console" && go test -race ./internal/iam/madminbackend)`

Expected: PASS。

Run: `(cd "console" && go test -tags integration ./internal/iam/madminbackend -run 'TestMadminContract' -count=1)`

Expected: PASS against an ephemeral current MinIO；测试结束清理其专用测试用户、组、策略和 Service Account，不连接生产。

**Step 6：建议提交检查点**

```bash
git add "console/internal/iam/madminbackend"
git commit -m "feat(console): adapt final madmin IAM API"
```

## Task 10：实现用户管理应用服务与 HTTP API

**Files:**

- Create: `console/internal/iam/users_service.go`
- Create: `console/api/iam_users.go`
- Modify: `console/api/configure_console.go`
- Test: `console/internal/iam/users_service_test.go`
- Test: `console/api/iam_users_test.go`

**Step 1：写用户用例失败测试**

覆盖列表、详情、创建、重置密码、启用、禁用和删除：

- 用户名规范化与 MinIO 规则一致；
- root 用户不可由该 API 修改或删除；
- 密码不进入 intent、event、error 或响应；
- 每个写操作必须经过 coordinator，返回 operation ID；
- 写成功后重新读取 MinIO，响应使用观察到的状态；
- 审计不可用时写操作不调用 madmin，读取仍成功；
- handler 精确验证 `CreateUser`、`DeleteUser`、`ListUsers`、`GetUser`、`EnableUser`、`DisableUser`。

Run: `(cd "console" && go test ./internal/iam ./api -run 'TestUsersService|TestIAMUsers')`

Expected: FAIL。

**Step 2：实现用户应用服务**

服务依赖小型 user backend、coordinator 和 clock。创建/重置密码的 canonical request 只包含用户名、动作和密码 HMAC 指纹，不保存密码。删除操作要求 handler 接收并匹配目标用户名确认字段。

**Step 3：实现 HTTP handler**

handler 只负责 session、权限、CSRF/HTTPS、请求解码/大小限制、调用 service 和生成 OpenAPI 响应。统一错误映射：认证 `401`、权限 `403`、冲突 `409`、校验 `422`、审计/未知 `503`。

**Step 4：验证用户全矩阵**

Run: `(cd "console" && go test -race ./internal/iam ./api -run 'TestUsersService|TestIAMUsers')`

Expected: PASS。

**Step 5：建议提交检查点**

```bash
git add "console/internal/iam/users_service.go" "console/internal/iam/users_service_test.go" "console/api/iam_users.go" "console/api/iam_users_test.go" "console/api/configure_console.go"
git commit -m "feat(console): add audited user management API"
```

## Task 11：实现组与成员管理应用服务和 HTTP API

**Files:**

- Create: `console/internal/iam/groups_service.go`
- Create: `console/api/iam_groups.go`
- Modify: `console/api/configure_console.go`
- Test: `console/internal/iam/groups_service_test.go`
- Test: `console/api/iam_groups_test.go`

**Step 1：写组语义失败测试**

覆盖：创建组、读取组、增加/移除成员、启用/禁用以及按最终 madmin 语义删除空组。测试保证：

- 成员集合标准化排序，重复成员不会产生重复 mutation；
- 删除非空组返回 `409 GROUP_NOT_EMPTY`；
- 增加成员校验 `admin:AddUserToGroup`，移除成员校验 `admin:RemoveUserFromGroup`；
- mutation 成功后重新读取组描述；
- 幂等重试不会重复更新成员；
- 审计只保存组名和受影响成员名，不保存任意请求体。

Run: `(cd "console" && go test ./internal/iam ./api -run 'TestGroupsService|TestIAMGroups')`

Expected: FAIL。

**Step 2：实现基于最终 API 的组操作**

通过 `UpdateGroupMembers` 表达创建、增加、移除和删除空组，不发明 MinIO 不支持的独立组实体语义。服务读取 `GetGroupDescription` 验证结果；状态通过 `SetGroupStatus` 变更。

**Step 3：实现 handler 与授权映射**

注册 OpenAPI route，应用 `ListGroups`、`GetGroup`、`AddUserToGroup`、`RemoveUserFromGroup`、`EnableGroup` 和 `DisableGroup` 的精确 Action 检查。破坏性删除要求输入组名确认。

**Step 4：验证组全矩阵**

Run: `(cd "console" && go test -race ./internal/iam ./api -run 'TestGroupsService|TestIAMGroups')`

Expected: PASS。

**Step 5：建议提交检查点**

```bash
git add "console/internal/iam/groups_service.go" "console/internal/iam/groups_service_test.go" "console/api/iam_groups.go" "console/api/iam_groups_test.go" "console/api/configure_console.go"
git commit -m "feat(console): add audited group management API"
```

## Task 12：实现策略、校验与绑定管理

**Files:**

- Create: `console/internal/iam/policies_service.go`
- Create: `console/internal/iam/bindings_service.go`
- Create: `console/api/iam_policies.go`
- Create: `console/api/iam_bindings.go`
- Modify: `console/api/configure_console.go`
- Test: `console/internal/iam/policies_service_test.go`
- Test: `console/internal/iam/bindings_service_test.go`
- Test: `console/api/iam_policies_test.go`

**Step 1：写策略与绑定失败测试**

覆盖：

- JSON 语法错误、未知 Action、无效资源 ARN 返回 `422 POLICY_INVALID`；
- `policy.DefaultPolicies` 中 `consoleAdmin`、`diagnostics`、`readonly`、`readwrite`、`writeonly` 标记为内置且只读；
- 自定义策略创建、更新、删除调用最终 madmin 方法；
- 删除有关联策略由 MinIO 拒绝并映射稳定冲突；
- 用户和组 attach/detach 使用明确主体类型，不能混淆；
- 策略正文不写 audit payload，审计只保存名称、规范化 SHA-256/HMAC 摘要和受影响主体；
- 写后通过 `InfoCannedPolicyV2` 或 `GetPolicyEntities` 重新读取确认。

Run: `(cd "console" && go test ./internal/iam ./api -run 'TestPoliciesService|TestBindingsService|TestIAMPolicies')`

Expected: FAIL。

**Step 2：复用 MinIO policy parser 做服务端校验**

禁止前端 JSON 编辑器成为唯一校验。后端使用根模块当前 policy 包可接受的解析和验证路径；canonical hash 基于解析后重新编码的 JSON，不能受空白或 map 顺序影响。

**Step 3：实现策略和绑定服务**

策略服务使用 `ListCannedPolicies`、`InfoCannedPolicyV2`、`AddCannedPolicy`、`RemoveCannedPolicy`。绑定服务使用 `GetPolicyEntities`、`AttachPolicy`、`DetachPolicy`，明确区分 user/group。内置策略只读检查在应用层完成，MinIO 仍负责最终授权和约束。

**Step 4：实现 HTTP API 与稳定错误**

为 validate endpoint 设置只解析不持久化的服务方法；create/update/delete/attach/detach 全部使用 coordinator、幂等 key 和写后读取。破坏性删除要求输入策略名确认。

**Step 5：验证策略与绑定**

Run: `(cd "console" && go test -race ./internal/iam ./api -run 'TestPoliciesService|TestBindingsService|TestIAMPolicies|TestIAMBindings')`

Expected: PASS。

**Step 6：建议提交检查点**

```bash
git add "console/internal/iam/policies_service.go" "console/internal/iam/bindings_service.go" "console/internal/iam" "console/api/iam_policies.go" "console/api/iam_bindings.go" "console/api/configure_console.go"
git commit -m "feat(console): manage policies and bindings"
```

## Task 13：实现 Access Key 一次性 Secret 和分阶段轮换

**Files:**

- Create: `console/internal/iam/access_keys_service.go`
- Create: `console/api/iam_access_keys.go`
- Modify: `console/api/configure_console.go`
- Test: `console/internal/iam/access_keys_service_test.go`
- Test: `console/api/iam_access_keys_test.go`
- Test: `console/api/secret_leak_test.go`

**Step 1：写一次性 Secret 失败测试**

覆盖：

- 创建调用 `AddServiceAccount`，Secret 只进入 `SecretMaterial`；
- 正常响应消费一次后内存清零；
- 同一幂等键重试不再返回 Secret，而返回 `409 SECRET_ALREADY_ISSUED` 与 operation ID；
- list/info 永远不含 Secret 字段；
- update-status 调用 `UpdateServiceAccount`，delete 调用 `DeleteServiceAccount`；
- root 主凭据和本地用户主凭据不在该服务范围；
- 日志、operation、audit event、JSON error、HTTP header 不出现 Secret；
- 创建结果无法审计或无法安全交付时进入 `unknown`，交给补偿路径而不是自动重建 Key。

Run: `(cd "console" && go test ./internal/iam ./api -run 'TestAccessKeysService|TestOneTimeSecret|TestSecretLeak')`

Expected: FAIL。

**Step 2：实现 Service Account 领域用例**

列表只映射 Access Key ID、父用户、状态、创建/过期时间和策略摘要。创建 request 可包含名称、描述、过期时间和受支持的会话策略，但 audit 只保存允许字段与策略摘要。

不实现“重取 Secret”或“覆盖 Secret”。轮换是四个已有用例的 UI 编排：创建新 Key、管理员切换客户端、禁用旧 Key、删除旧 Key；每步拥有独立 operation ID 和审计事件。

**Step 3：实现 Secret 响应专用 writer**

专用 writer 在写 body 前设置 `no-store/no-cache/no-referrer`，只从 `SecretMaterial.Consume()` 构造一次响应 DTO；写完立即清零临时字节。若连接在响应过程中断，不重放、不缓存 Secret，operation 保持可核对状态。

**Step 4：实现 API 与确认语义**

创建、禁用和删除都要求 HTTPS、CSRF、精确 Admin Action 和幂等键。删除要求输入 Access Key ID 确认。创建请求不得接受客户端指定 Secret。

**Step 5：运行内存、HTTP 和泄漏扫描测试**

Run: `(cd "console" && go test -race ./internal/iam ./api -run 'TestAccessKeysService|TestOneTimeSecret|TestSecretLeak')`

Expected: PASS。

Run: `rg -n -i 'secret.?key|password|authorization|cookie|dsn' "console/internal/iam" "console/api"`

Expected: 所有命中逐条审阅；只允许字段拒绝列表、类型名、测试假值或受控敏感处理代码，不允许日志格式和持久化 model 字段。

**Step 6：建议提交检查点**

```bash
git add "console/internal/iam/access_keys_service.go" "console/internal/iam/access_keys_service_test.go" "console/api/iam_access_keys.go" "console/api/iam_access_keys_test.go" "console/api/secret_leak_test.go" "console/api/configure_console.go"
git commit -m "feat(console): manage one-time Access Keys"
```

## Task 14：实现核对、审计查询、配置和后台维护

**Files:**

- Create: `console/internal/iam/reconciler.go`
- Create: `console/internal/iam/audit_service.go`
- Create: `console/internal/iam/worker.go`
- Create: `console/api/iam_operations.go`
- Create: `console/api/iam_audit.go`
- Create: `console/api/iam_health.go`
- Modify: `console/api/config.go`
- Modify: `console/api/configure_console.go`
- Modify: `cmd/common-main.go`
- Test: `console/internal/iam/reconciler_test.go`
- Test: `console/internal/iam/worker_test.go`
- Test: `console/api/iam_audit_test.go`
- Test: `cmd/common-main_test.go`

**Step 1：写 reconciler 决策失败测试**

对每种 mutation 定义明确 `DesiredState` 和补偿策略，覆盖：

- 观察值等于目标值：`reconciled_succeeded`；
- 明确没有达到目标：`reconciled_failed`；
- 仍无法读取或存在歧义：`manual_review`，不得循环重放 mutation；
- Access Key 创建已存在但 Secret 未能交付：优先调用禁用，确认后 `compensated`；禁用也无法确认则 `manual_review`；
- 两节点同时领取同一 operation 时只有持有行锁的一方处理；
- 已进入终态的 operation 不再处理；
- 每次结果追加 `reconcile` 或 `compensation` event。

Run: `(cd "console" && go test ./internal/iam -run 'TestReconciler')`

Expected: FAIL。

**Step 2：实现有界核对 worker**

worker 获取集群 advisory lock 后，以固定批量和 `FOR UPDATE SKIP LOCKED` 领取过期 operation。每轮有 context timeout、最大批量和退避；节点失去数据库连接后锁自然释放。worker 只调用 `Observe` 或明确的 Access Key 补偿，不调用原 mutation 重试。

**Step 3：写审计和操作查询失败测试**

覆盖按时间、actor、Action、resource、status、node 和 operation ID 过滤；验证：

- 稳定 cursor 分页无重复/遗漏；
- operation timeline 按 sequence 排序；
- safe payload 不包含策略正文、Secret、密码或 DSN；
- 普通用户和权限不完整用户返回 `403`；
- 数据库不可用时 audit endpoint 返回独立 `503`，IAM 实体读取不受影响。

Run: `(cd "console" && go test ./internal/iam ./api -run 'TestAuditService|TestIAMOperations|TestIAMAudit')`

Expected: FAIL。

**Step 4：实现配置解析与降级状态**

集中解析：

- `MINIO_CONSOLE_AUDIT_DB_TYPE`：生产只接受 `postgres`；测试显式 `sqlite`；
- `MINIO_CONSOLE_AUDIT_DSN`：敏感包装，任何格式化只输出 redacted；
- `MINIO_CONSOLE_AUDIT_RETENTION_DAYS`：正整数，默认 365，并设置合理上限；
- `MINIO_CONSOLE_AUDIT_NODE_ID`：可选，否则从 MinIO 节点身份稳定派生；
- 可信代理 CIDR、worker 间隔、连接池上限等已由设计要求直接需要的运行参数。

错误配置或 migration 失败不终止 S3 服务；`IAMRuntime` 进入写保护并暴露 MinIO IAM readable、audit DB、schema compatibility、write enabled、reconciler/partition last-success 状态。日志不得打印 DSN。

**Step 5：接入根 MinIO 生命周期**

在 `cmd/common-main.go` 构造 Console 配置时注入 cluster-stable 派生密钥、节点身份和运行配置。Console 启动/关闭负责建立有界 DB pool、运行迁移检查、启动 reconciler/partition worker 并在 MinIO context 取消时有界退出。不得新增独立监听器或服务进程。

**Step 6：验证后台任务、配置和降级**

Run: `(cd "console" && go test -race ./internal/iam ./api -run 'TestReconciler|TestWorker|TestAuditService|TestIAMHealth')`

Expected: PASS。

Run: `go test -tags kqueue,dev ./cmd -run 'TestConsoleIAMConfig|TestConsoleServerConfig'`

Expected: PASS。

Run: `(cd "console" && go test -race -tags integration ./internal/iam/... ./api/... -run 'TestMultiNodeReconcile|TestPartitionMaintainer' -count=1)`

Expected: PASS against PostgreSQL。

**Step 7：建议提交检查点**

```bash
git add "console/internal/iam" "console/api" "cmd/common-main.go" "cmd/common-main_test.go"
git commit -m "feat(console): reconcile and query IAM audit operations"
```

## Task 15：恢复前端 IAM 导航、权限门控和安全 API 基础

**Files:**

- Modify: `console/web-app/src/screens/Console/Console.tsx`
- Modify: `console/web-app/src/screens/Console/Menu/MenuWrapper.tsx`
- Modify: `console/web-app/src/screens/Console/ConsoleKBar.tsx`
- Modify: `console/web-app/src/store.ts`
- Modify: `console/web-app/src/systemSlice.ts`
- Modify: `console/web-app/src/common/SecureComponent/permissions.ts`
- Modify: `console/web-app/src/screens/LoginPage/Login.tsx`
- Modify: `console/web-app/src/screens/Console/HelpMenu.tsx`
- Delete: `console/web-app/src/screens/Console/License/`
- Create: `console/web-app/src/screens/Console/IAM/routes.tsx`
- Create: `console/web-app/src/screens/Console/IAM/IAMGuard.tsx`
- Create: `console/web-app/src/screens/Console/IAM/iamPermissions.ts`
- Create: `console/web-app/src/screens/Console/IAM/iamApi.ts`
- Create: `console/web-app/src/screens/Console/IAM/operationPoller.ts`
- Create: `console/web-app/src/screens/Console/IAM/OneTimeSecretDialog.tsx`
- Test: `console/web-app/src/screens/Console/IAM/IAMGuard.test.tsx`
- Test: `console/web-app/src/screens/Console/IAM/iamApi.test.ts`
- Test: `console/web-app/src/screens/Console/IAM/OneTimeSecretDialog.test.tsx`

**Step 1：写路由与权限门控失败测试**

基于 session response 的 `Permissions` 验证：

- 完整 IAM Action 集显示 `Identity & Access` 导航和 Users/Groups/Policies/Access Keys/Audit 子项；
- 缺少入口 Action 集时不显示且直接访问 route 返回禁止页；
- 页面按钮按精确 Action 启用，前端门控不替代后端 `403`；
- 侧边栏不再渲染 `Documentation` 与 `License` 入口，且 `/license` 路由不可达；
- Object Browser 与 Buckets 原路由保持不变。

Run: `(cd "console/web-app" && yarn test IAMGuard.test.tsx --runInBand)`

Expected: FAIL。

**Step 2：从 v1.7.6 选择性移植 UI 模式**

复用历史 `Users`、`Groups`、`Policies` 和 Service Account 页面中的 MinIO Design System 布局、表格和对话框模式，但统一迁入 `screens/Console/IAM/` 并改用新 `/api/v1/iam` client。不要恢复历史全局设置、IDP、监控、KMS 或事件页面。

**Step 3：移除 License 与 Documentation 导航入口**

`MenuWrapper.tsx` 删除 `endComponent` 中的 `Documentation` 与 `License` 两个 `MenuItem`，并清理随之
失效的 `DocumentationIcon`、`LicenseIcon` 与 `getLicenseConsent` 引用。`MenuItem` 仍被 `Create Bucket`
使用，不得一并删除。

同时移除 License 页面本体，避免入口消失后路由仍可直接访问：

- 删除 `console/web-app/src/screens/Console/License/`；
- `Console.tsx` 移除 `License` 的 `React.lazy` 导入与 `IAM_PAGES.LICENSE` 路由项；
- `ConsoleKBar.tsx` 移除 `LicenseConsentModal` 的导入与渲染；
- `store.ts` 移除 `licenseReducer`；
- `systemSlice.ts` 移除 `licenseAcknowledged` 与 `SubnetInfo` 引用；
- `common/SecureComponent/permissions.ts` 移除 `IAM_PAGES.LICENSE` 及其权限映射。

其余 Documentation 外链一并去掉：`screens/LoginPage/Login.tsx` 的页脚链接、`HelpMenu.tsx` 的
`Visit MinIO Documentation` 项与 `Documentation` tab。

不得保留“仅隐藏入口、路由仍可达”的中间状态，也不得为了删除而放宽 `IAM_PAGES` 的类型约束。

**Step 4：实现安全请求层**

`iamApi.ts` 只封装生成 client 的会话/CSRF、UUID 幂等键、稳定错误和 operation ID。禁止自动重试写请求；网络不确定时返回 operation 查询入口。读取可以使用有界重试。

`operationPoller.ts` 只查询已有 operation，使用有界指数退避并允许用户停止；不得重放 mutation。

**Step 5：实现一次性 Secret 对话框**

Secret 只存于组件局部 state/ref，不进入 Redux。对话框支持一次复制、关闭确认和不可恢复提示；关闭、unmount、路由切换后清空内存引用。禁止 Local Storage、Session Storage、URL 参数和下载历史保存 Secret。

测试 monkey-patch 浏览器 storage，断言创建/复制/关闭期间没有对 Secret 执行持久化写入；rerender 和刷新后不能恢复 Secret。

**Step 6：验证基础前端安全**

Run: `(cd "console/web-app" && yarn test IAMGuard.test.tsx iamApi.test.ts OneTimeSecretDialog.test.tsx --runInBand)`

Expected: PASS。

Run: `(cd "console/web-app" && yarn build)`

Expected: PASS，TypeScript 和嵌入静态资源生成成功；删除 License 页面后不得残留未解析导入，
`web-app` 既有死代码检查也不得因此新增告警。

**Step 7：建议提交检查点**

```bash
git add "console/web-app/src/screens/Console/Console.tsx" "console/web-app/src/screens/Console/IAM" \
  "console/web-app/src/screens/Console/Menu/MenuWrapper.tsx" "console/web-app/src/screens/Console/ConsoleKBar.tsx" \
  "console/web-app/src/screens/Console/HelpMenu.tsx" "console/web-app/src/screens/LoginPage/Login.tsx" \
  "console/web-app/src/store.ts" "console/web-app/src/systemSlice.ts" \
  "console/web-app/src/common/SecureComponent/permissions.ts" "console/web-app/src/screens/Console/License"
git commit -m "feat(console): restore secure IAM navigation"
```

## Task 16：实现 Users 与 Groups 页面

**Files:**

- Create: `console/web-app/src/screens/Console/IAM/Users/UsersPage.tsx`
- Create: `console/web-app/src/screens/Console/IAM/Users/UserDetails.tsx`
- Create: `console/web-app/src/screens/Console/IAM/Users/UserForm.tsx`
- Create: `console/web-app/src/screens/Console/IAM/Groups/GroupsPage.tsx`
- Create: `console/web-app/src/screens/Console/IAM/Groups/GroupDetails.tsx`
- Create: `console/web-app/src/screens/Console/IAM/Groups/GroupMembersForm.tsx`
- Test: `console/web-app/src/screens/Console/IAM/Users/UsersPage.test.tsx`
- Test: `console/web-app/src/screens/Console/IAM/Groups/GroupsPage.test.tsx`

**Step 1：写 Users 页面失败测试**

覆盖搜索、状态过滤、分页、创建、密码重置、启用、禁用、删除和详情中组/策略/Access Key 关联展示。断言：

- 密码输入不进入全局 state；
- 成功后重新 fetch MinIO 状态；
- 删除必须输入用户名；
- `unknown` 响应展示 operation ID 和查询状态入口；
- 缺少对应 Action 时按钮不可见或禁用。

Run: `(cd "console/web-app" && yarn test UsersPage.test.tsx --runInBand)`

Expected: FAIL。

**Step 2：实现 Users 页面**

复用 v1.7.6 的表格和详情交互，不复用其旧 API thunk。列表状态仅保存非敏感过滤器和分页；所有 mutation 走 `iamApi`，完成后 invalidation/refetch。

**Step 3：写 Groups 页面失败测试**

覆盖创建、成员增加/移除、启用/禁用、绑定展示和空组删除。断言删除非空组冲突可读，破坏性操作必须输入组名。

Run: `(cd "console/web-app" && yarn test GroupsPage.test.tsx --runInBand)`

Expected: FAIL。

**Step 4：实现 Groups 页面**

成员选择器从实时用户列表加载，提交时发送明确 add/remove 集合；请求成功后重新读取组详情。不得在浏览器模拟 MinIO 最终成员状态。

**Step 5：验证 Users/Groups 与对象页面回归**

Run: `(cd "console/web-app" && yarn test UsersPage.test.tsx GroupsPage.test.tsx --runInBand)`

Expected: PASS。

Run: `(cd "console/web-app" && yarn test --watchAll=false --runInBand)`

Expected: PASS，现有 Bucket/Object 单元测试无回归。

**Step 6：建议提交检查点**

```bash
git add "console/web-app/src/screens/Console/IAM/Users" "console/web-app/src/screens/Console/IAM/Groups"
git commit -m "feat(console): add user and group administration pages"
```

## Task 17：实现 Policies 与 Access Keys 页面

**Files:**

- Create: `console/web-app/src/screens/Console/IAM/Policies/PoliciesPage.tsx`
- Create: `console/web-app/src/screens/Console/IAM/Policies/PolicyEditor.tsx`
- Create: `console/web-app/src/screens/Console/IAM/Policies/PolicyBindings.tsx`
- Create: `console/web-app/src/screens/Console/IAM/AccessKeys/AccessKeysPage.tsx`
- Create: `console/web-app/src/screens/Console/IAM/AccessKeys/CreateAccessKey.tsx`
- Create: `console/web-app/src/screens/Console/IAM/AccessKeys/RotateAccessKey.tsx`
- Test: `console/web-app/src/screens/Console/IAM/Policies/PoliciesPage.test.tsx`
- Test: `console/web-app/src/screens/Console/IAM/AccessKeys/AccessKeysPage.test.tsx`

**Step 1：写 Policies 页面失败测试**

覆盖内置/自定义标识、JSON 编辑、服务端 validate、创建/更新/删除、用户/组绑定与解绑。断言内置策略编辑和删除按钮始终不可用，策略保存成功后重新获取规范化服务端文档。

Run: `(cd "console/web-app" && yarn test PoliciesPage.test.tsx --runInBand)`

Expected: FAIL。

**Step 2：实现 Policies 页面**

编辑器可以做客户端 JSON 语法提示，但提交前必须调用服务端 validate。绑定组件显式选择 user/group，显示后端返回的实时关联；不根据策略名称推断管理员权限。

**Step 3：写 Access Keys 与轮换失败测试**

覆盖列表、创建一次性 Secret、禁用、删除和四步轮换引导。断言：

- 列表不渲染 Secret 列；
- 创建响应只进入 `OneTimeSecretDialog`；
- 对话框关闭后 DOM、React state mock、Redux store 和 storage 中均不存在 Secret；
- 幂等重试收到 `SECRET_ALREADY_ISSUED` 时不伪造 Secret；
- 轮换不会自动禁用或删除旧 Key，每一步要求管理员明确操作；
- 删除要求输入 Access Key ID。

Run: `(cd "console/web-app" && yarn test AccessKeysPage.test.tsx OneTimeSecretDialog.test.tsx --runInBand)`

Expected: FAIL。

**Step 4：实现 Access Keys 页面**

列表显示 ID、父用户、状态、创建时间、过期时间和策略摘要。创建页不提供客户端 Secret 输入。轮换组件只编排现有 API，并持续展示新旧 Key ID 和每步 operation 状态，不保存 Secret。

**Step 5：验证 Policies/Access Keys**

Run: `(cd "console/web-app" && yarn test PoliciesPage.test.tsx AccessKeysPage.test.tsx OneTimeSecretDialog.test.tsx --runInBand)`

Expected: PASS。

Run: `(cd "console/web-app" && yarn build)`

Expected: PASS。

**Step 6：建议提交检查点**

```bash
git add "console/web-app/src/screens/Console/IAM/Policies" "console/web-app/src/screens/Console/IAM/AccessKeys"
git commit -m "feat(console): add policy and Access Key pages"
```

## Task 18：实现 Audit 页面与异常操作时间线

**Files:**

- Create: `console/web-app/src/screens/Console/IAM/Audit/AuditPage.tsx`
- Create: `console/web-app/src/screens/Console/IAM/Audit/AuditFilters.tsx`
- Create: `console/web-app/src/screens/Console/IAM/Audit/OperationTimeline.tsx`
- Create: `console/web-app/src/screens/Console/IAM/Audit/AuditHealthBanner.tsx`
- Test: `console/web-app/src/screens/Console/IAM/Audit/AuditPage.test.tsx`
- Test: `console/web-app/src/screens/Console/IAM/Audit/OperationTimeline.test.tsx`

**Step 1：写 Audit 页面失败测试**

覆盖按时间、actor、Action、resource、status、node 和 operation ID 过滤、cursor 翻页、事件详情和时间线。断言：

- `pending`、`unknown`、`manual_review` 使用不同醒目标识；
- operation 展示 intent/result/reconcile/compensation 顺序；
- 数据库降级时只显示 IAM 审计不可用 banner，不误报整个 MinIO 离线；
- payload renderer 使用键白名单，不渲染未识别字段；
- 权限不完整的主体不能访问页面。

Run: `(cd "console/web-app" && yarn test AuditPage.test.tsx OperationTimeline.test.tsx --runInBand)`

Expected: FAIL。

**Step 2：实现 Audit 页面**

URL 查询参数只保存非敏感过滤器；详情通过 event/operation ID 获取。刷新异常 operation 只执行 GET 查询，不触发 mutation。后端返回的安全字段仍经过前端允许键 renderer，避免未来后端字段意外暴露。

**Step 3：验证审计 UI 和全量前端测试**

Run: `(cd "console/web-app" && yarn test AuditPage.test.tsx OperationTimeline.test.tsx --runInBand)`

Expected: PASS。

Run: `(cd "console/web-app" && yarn test --watchAll=false --runInBand)`

Expected: PASS。

Run: `(cd "console/web-app" && yarn build)`

Expected: PASS。

**Step 4：建议提交检查点**

```bash
git add "console/web-app/src/screens/Console/IAM/Audit"
git commit -m "feat(console): add IAM audit operation timeline"
```

## Task 19：完成端到端、多节点、发布和 10.0.1.119 部署验证

**Files:**

- Create: `console/web-app/e2e/iam-users-groups.spec.ts`
- Create: `console/web-app/e2e/iam-policies-keys.spec.ts`
- Create: `console/web-app/e2e/iam-audit-security.spec.ts`
- Create: `buildscripts/verify-console-secrets.sh`
- Create: `docs/operations/console-iam-postgres.md`
- Create: `docs/operations/console-iam-deployment.md`
- Modify: `docs/console-development.md`
- Modify: `.github/workflows/**`（只修改现有匹配的测试/构建工作流）

**Step 1：写端到端失败测试**

Playwright 使用专用测试 MinIO 与 PostgreSQL，覆盖：

- root 登录后用户、组、成员、策略、绑定完整流程；
- 创建 Access Key、一次性 Secret、刷新后不可恢复、禁用和删除；
- 分阶段轮换，新 Key 创建后旧 Key 保持启用，只有明确确认后才禁用/删除；
- audit timeline 包含各操作；
- 普通用户和缺少单个 Action 的用户均无法绕过 UI 或直接 API；
- 地址始终保持 Console `9001`，网络请求不把浏览器重定向到 `9000`；
- Bucket 浏览、上传、下载和对象操作继续工作。

Run: `(cd "console/web-app" && yarn playwright install --with-deps chromium)`

Expected: 浏览器依赖就绪；该包管理/网络步骤执行前按仓库规则取得确认。

Run: `(cd "console/web-app" && yarn playwright test e2e/iam-users-groups.spec.ts e2e/iam-policies-keys.spec.ts e2e/iam-audit-security.spec.ts)`

Expected: FAIL before feature wiring is complete。

**Step 2：增加多节点故障注入 harness**

在现有集成测试设施中启动两个 MinIO 节点/Console 实例共享一个 PostgreSQL，使用受控 fault hooks 测试：

- 相同幂等键落到不同节点只执行一次；
- intent 前 DB 断开时 mutation 次数为零；
- Admin API 超时不自动重放；
- Admin 成功后 result DB 失败转 unknown；
- 节点崩溃释放 advisory lock，另一节点接管 reconcile/partition；
- Secret 响应丢失触发禁用/删除补偿或 manual review；
- 旧分区存在未收敛操作时不删除，收敛后按 retention 删除。

Run: `(cd "console" && go test -race -tags integration ./internal/iam/... ./api/... -run 'TestMultiNode|TestFaultInjection|TestRetention' -count=1)`

Expected: PASS against PostgreSQL and ephemeral MinIO only。

**Step 3：增加 Secret 静态与运行产物扫描**

`buildscripts/verify-console-secrets.sh` 扫描测试数据库 dump、服务日志、Playwright trace/HAR、浏览器 storage dump 和构建产物，使用测试中生成的唯一 marker 精确判断泄漏。脚本不得打印 marker 原值，只报告文件和敏感类别。

Run: `bash "buildscripts/verify-console-secrets.sh" "<test-artifact-directory>"`

Expected: PASS，所有受检位置零 marker 命中。

**Step 4：完成 CI 与文档**

CI 分为：

- Console Go/SQLite 快速测试；
- PostgreSQL migration/partition/multi-node integration；
- React/Jest/build；
- Playwright IAM 与现有对象回归；
- 根 `make verifiers`、`make build`、相关 `make test`；
- `CGO_ENABLED=0` 生产构建和本地 Console replace 验证。

`docs/operations/console-iam-postgres.md` 记录专用 schema、最小权限、TLS DSN、备份、迁移锁、分区、保留和恢复。`docs/operations/console-iam-deployment.md` 记录单二进制升级/回滚、健康检查和 unknown/manual_review 处理，不写真实凭据。

**Step 5：执行完整发布候选验证**

Run: `(cd "console" && go test -race ./internal/iam/... ./api/...)`

Expected: PASS。

Run: `(cd "console" && go test -race -tags integration ./internal/iam/... ./api/... -count=1)`

Expected: PASS against isolated PostgreSQL/MinIO。

Run: `(cd "console/web-app" && yarn test --watchAll=false --runInBand)`

Expected: PASS。

Run: `(cd "console/web-app" && yarn playwright test)`

Expected: PASS。

Run: `make verifiers`

Expected: PASS。

Run: `make build`

Expected: PASS，根目录只发布 `minio`。

Run: `go test -tags kqueue,dev ./cmd ./internal/...`

Expected: PASS。若完整 `make test` 需要 Docker/外部服务，先检查对应脚本并记录明确的环境前置条件，不把环境缺失描述为测试成功。

**Step 6：部署前危险操作确认**

目标主机已验证的当前形态：

- SSH：`rain@10.0.1.119`；
- user unit：`/home/rain/.config/systemd/user/minio.service`；
- 二进制：`/home/rain/.local/bin/minio`；
- 环境文件：`/home/rain/.config/minio/minio.env`；
- 数据目录：`/home/rain/.local/share/minio/data`；
- ExecStart：`/home/rain/.local/bin/minio server /home/rain/.local/share/minio/data --address :9000 --console-address :9001`。

部署会修改生产二进制、环境文件、PostgreSQL schema 并重启服务，必须单独输出：

```text
⚠️ Dangerous Operation Detected
Operation Type: Production database migration, systemd environment update, binary replacement and service restart
Impact Scope: rain@10.0.1.119 MinIO :9000/:9001 and the dedicated Console audit PostgreSQL schema
Risk Assessment: A bad binary, migration, TLS/DSN setting or restart can make IAM writes unavailable; MinIO data must not be modified by the deployment procedure

Please confirm to continue? [requires explicit "yes", "confirm", "continue"]
```

未取得确认时停在本步骤，不创建数据库、不改环境文件、不替换二进制、不重启服务。

**Step 7：确认后执行可恢复部署**

确认后按以下顺序：

1. 只读记录当前 binary checksum、version、unit 内容、环境变量名称和服务状态，不输出敏感值；
2. 将旧二进制和 unit/env 文件复制到带 UTC 时间戳且权限受限的备份目录；
3. 用专用 PostgreSQL 账号建立 `minio_console` schema 权限和 TLS 连接，运行同一发布物携带的 migration preflight；
4. 原子替换 `/home/rain/.local/bin/minio`，保持 owner/mode；
5. 仅在环境文件加入已批准的 Console audit 配置，DSN 不回显；
6. `systemctl --user daemon-reload` 并重启同一个 `minio.service`；
7. 失败时恢复旧 binary/env/unit，重启同一服务；不自动执行破坏性 down migration。

**Step 8：部署后验证**

只读验证：

- `systemctl --user is-active minio.service` 为 `active`，且只有一个主 PID；
- `9000` 返回 S3 XML，`9001` 返回 Console 且不跳转；
- 现有 Bucket 浏览与对象操作回归；
- IAM 健康显示 database/schema/write enabled；
- 使用专用验收主体完成 user/group/policy/binding/Access Key/审计流程；
- 测试 Secret marker 不出现在服务日志、数据库和浏览器持久化；
- 两节点环境可用时验证跨节点幂等与 worker leader；单节点目标不能冒充多节点验收。

验收完成后删除专用测试 IAM 实体属于生产数据变更，需包含在部署确认范围内并逐项审计；不删除用户既有对象或 IAM 数据。

**Step 9：建议提交检查点**

```bash
git add "console/web-app/e2e" "buildscripts/verify-console-secrets.sh" "docs/operations" "docs/console-development.md" ".github/workflows"
git commit -m "test: verify Console IAM release workflow"
```

## 最终完成判定

只有同时满足下列条件才可宣称功能完成：

1. 根模块确认使用仓库内 `console/`，源码来源和校验值可审计。
2. 根构建只产出一个 `minio`，同一 PID 提供 `9000` 与 `9001`。
3. 后端 Action 级权限、CSRF、可信代理、HTTPS 门控和请求限制测试通过。
4. 用户、组、策略、绑定、Access Key 及审计的 Go、Jest 和 Playwright 流程通过。
5. IAM 实体始终从 MinIO 读取，PostgreSQL 中不存在第二份 IAM 实体表。
6. PostgreSQL migration、月分区、365 天默认保留、保护状态和 advisory lock 测试通过。
7. 多节点幂等、未知结果核对、Access Key 补偿和节点接管故障测试通过。
8. Secret marker 扫描在数据库、日志、trace、缓存、browser storage 和构建产物中均为零命中。
9. 现有 Bucket/Object 浏览、上传、下载与 S3 API 回归通过。
10. 生产部署完成后，以真实输出记录 systemd、端口、IAM 健康和回滚检查结果；未部署时只可称“实现完成、待部署”，不能称生产完成。
