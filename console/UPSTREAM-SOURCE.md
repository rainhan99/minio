# Console 上游来源说明

本目录是 `github.com/minio/console` 的**内嵌本地 Go 模块**，由根模块 `github.com/minio/minio`
通过 `replace github.com/minio/console => ./console` 绑定。目录保留独立模块身份（`console/go.mod`
的 `module` 仍为 `github.com/minio/console`），不属于根模块的包树。

## 锁定来源

| 项目 | 值 |
| --- | --- |
| 模块路径 | `github.com/minio/console` |
| 模块版本（最终基线） | `v1.7.7-0.20250905210349-2017f33b26e1` |
| 伪版本对应上游提交 | `2017f33b26e1` |
| 伪版本时间戳（UTC） | `2025-09-05 21:03:49` |
| 模块校验值 | `h1:jOW1ggtITn8sreTzUjcdYE/ZffxeVmWstXNlBLOE6j4=` |
| `go.mod` 校验值 | `h1:hKNkzdKBKU84w5wXqMnkH74QocJGHW2zjvFtuGETDsc=` |
| 恢复日期 | 2026-08-18 |
| 恢复方式 | 从本机 Go module cache 中该锁定版本的解包目录整树复制 |
| 恢复规模 | 665 个文件、113 个目录、约 33 MB |
| 完整性核对 | 全部 665 个文件的 SHA-256 与来源逐一相同（`diff -r` 无内容差异） |

上述两个校验值与迁入前根 `go.sum` 中记录的值一致，也与实施计划
`docs/superpowers/plans/2026-08-17-minio-console-iam-management.md` 的「锁定基线与来源」一致。
迁入后根 `go.sum` 中这两行被 `go mod tidy` 移除（模块改为本地解析，不再需要校验和），
因此本文件是该校验值此后唯一的可审计记录。

## 历史 IAM 移植参考（未迁入）

| 项目 | 值 |
| --- | --- |
| 参考版本 | 官方 `github.com/minio/console v1.7.6` |
| 模块校验值 | `h1:E0jq9nYMeW7z4iJtJ6vDt2hk4Jin0zcyAzRcTlaUO44=` |
| 状态 | **未恢复到本目录**，仅作行为参考 |

v1.7.6 只用于参考 IAM handler、Swagger 契约和前端页面行为。最终模块（`2017f33b26e1`）
始终是主要源码；两者冲突时以最终模块为准，不得把 v1.7.6 的文件整体合并进来。

## 未恢复功能清单

以下功能域在最终模块中已由上游裁剪，本仓库**不恢复、不从 v1.7.6 移植**：

- **IDP（身份提供方）**：OpenID / LDAP 配置与管理页面。
- **监控**：指标、Prometheus 面板、Trace、Heal、Inspect、Profiling、Watch 相关的监控页面与看板。
- **KMS**：密钥管理服务的密钥、策略、身份管理。
- **事件目标**：Notification / Event Destination（AMQP、Kafka、MQTT、Webhook 等）配置与订阅。
- **设置中心**：服务端配置项集中编辑（`Settings` 页面及其子配置面板）。

此外，Operator / 多租户（Tenant）管理能力同样不在本次范围内。本次只按实施计划恢复并自主维护
IAM 管理（用户、组、策略、Access Key）与审计所需的部分，以及最终模块已有的 Bucket 与对象浏览能力。

## 本地改动记录

迁入时对来源树做的**全部**改动如下，业务代码零修改（未格式化、未 `go generate`、未升级任何依赖）：

1. `console/go.mod`：仅对齐工具链指令，`go 1.24.0` → `go 1.25.0`，`toolchain go1.24.4` → `toolchain go1.26.6`。
   `go mod tidy -compat=1.21 -diff` 在改动后无输出，依赖图与 `console/go.sum` 均未变化。
2. 新增本文件 `console/UPSTREAM-SOURCE.md`。
3. 权限规范化：module cache 解包为只读（目录 `555`、文件 `444`），迁入后统一为目录 `755`、文件 `644`。
   这是传输属性清理，不改内容。module cache 不保留可执行位，因此 `*.sh` 未带 `+x`；
   `console/Makefile` 一律以 `env bash <script>` 调用，不依赖可执行位。

许可与凭证文件 `LICENSE`、`NOTICE`、`CREDITS`，生成器（`hack/`、`swagger.yml`、`.license.tmpl`），
前端锁文件（`web-app/yarn.lock`、根 `yarn.lock`）以及预构建前端产物 `web-app/build/`
（被 `web-app/assets.go` 的 `//go:embed build/*` 使用）均已完整保留。

## 后续维护约定

- 本目录是自主维护的源码，不再从上游拉取更新；如需同步上游，必须重新走一次带校验值记录的迁入流程并更新本文件。
- 不得从来源不明的第三方 fork 合并文件。
- 工具链策略见根仓库 `buildscripts/verify-go-toolchain`；本模块的 `go` / `toolchain` 指令必须与根模块保持一致。
- 本目录的来源与工具链约束由 `buildscripts/verify-console-local-module.sh` 守护。
