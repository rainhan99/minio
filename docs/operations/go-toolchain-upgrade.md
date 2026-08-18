# Go 1.25/1.26 工具链升级运行手册

本文定义 MinIO 根模块从 Go 1.24 基线迁移后的验证、发布和回滚边界。Task 0 只产出经过
验证的候选代码与发布门禁，不授权数据库变更、systemd 修改或向 `10.0.1.119` 部署。

## 1. 版本契约

- 根模块语言与标准库兼容边界：`go 1.25.0`；
- 最低持续验证补丁版本：Go 1.25.13；
- 唯一生产发布工具链：Go 1.26.6；
- CI 必须设置 `GOTOOLCHAIN=local`，禁止测试阶段自动切换工具链；
- 正式构建必须为 `CGO_ENABLED=0`，且不得设置永久 `GODEBUG` 或
  `GOEXPERIMENT=nogreenteagc`。

版本升级以仓库策略检查器为单一真相源：

```bash
GOTOOLCHAIN=go1.26.6 go run ./buildscripts/verify-go-toolchain -root .
```

## 2. 开发与 CI 验证

### 2.1 源码策略和双版本验证

在同一提交上分别执行：

```bash
GOTOOLCHAIN=go1.25.13 go version
GOTOOLCHAIN=go1.25.13 go mod tidy -compat=1.21 -diff
GOTOOLCHAIN=go1.25.13 go vet -tags kqueue,dev ./internal/handlers ./cmd
GOTOOLCHAIN=go1.25.13 go test ./buildscripts/verify-go-toolchain ./internal/handlers
GOTOOLCHAIN=go1.25.13 go test -tags kqueue,dev ./cmd \
  -run '^Test(TrackingResponseWriter|HeadersAlreadyWritten|HeadersAlreadyWrittenWrapped|CreateEndpoints)$' \
  -count=1

GOTOOLCHAIN=go1.26.6 go version
GOTOOLCHAIN=go1.26.6 go mod tidy -compat=1.21 -diff
GOTOOLCHAIN=go1.26.6 go test ./buildscripts/verify-go-toolchain ./internal/handlers
GOTOOLCHAIN=go1.26.6 go test -tags kqueue,dev ./cmd -count=1
GOTOOLCHAIN=go1.26.6 go test -race ./internal/handlers -run '^TestForwarder' -count=1
GOTOOLCHAIN=go1.26.6 go test -race -tags kqueue,dev ./cmd \
  -run '^Test(CreateEndpoints|TrackingResponseWriter)$' -count=1
GOTOOLCHAIN=go1.26.6 make verifiers
GOTOOLCHAIN=go1.26.6 bash buildscripts/cross-compile.sh
```

GitHub Actions 的职责边界如下：

| 门禁 | 工具链 | 产物 | 要求 |
| --- | --- | --- | --- |
| `.github/workflows/go-compat.yml` | 1.25.13 | 仅临时 Linux amd64/arm64 编译物 | 最低版本、tidy、关键测试和静态编译 |
| 现有 Go workflows | 1.26.6 | 按原 workflow 职责 | 精确版本断言、完整验证及集成测试 |
| 正式发布入口 | 1.26.6 | 唯一 `minio` 二进制 | 干净提交、CGO 关闭、元数据严格匹配 |

### 2.2 发布构建与元数据

正式候选只能从普通的独立干净检出构建。不要从 linked worktree 生成候选：Go 的 VCS
stamping 可能读取 common Git directory 对应的主工作区 revision，使二进制 revision 与当前
feature commit 不一致。

```bash
make build-release
```

也可以显式执行等价检查：

```bash
GOTOOLCHAIN=go1.26.6 CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -tags kqueue -trimpath -o ./minio .
GOTOOLCHAIN=go1.26.6 go run ./buildscripts/verify-go-toolchain \
  -root . -binary ./minio -revision "$(git rev-parse HEAD)" \
  -goos linux -goarch amd64
```

严格检查必须证明：

- 编译器为 `go1.26.6`；
- `CGO_ENABLED=0`，目标平台与候选名称一致；
- `vcs.revision` 等于发布提交，`vcs.modified=false`；
- `DefaultGODEBUG` 包含 `cryptocustomrand=1`、`tlssecpmlkem=0`、
  `urlstrictcolons=0`；
- `containermaxprocs`、`updatemaxprocs`、`tlssha1`、`x509sha256skid` 等未被显式写回旧值，
  即沿用 `go 1.25` 指令对应的新默认行为。

`-allow-modified` 只用于开发期确认其他元数据，生成的二进制不得部署。

## 3. 开发侧性能信号

### 3.1 可重复流程

必须使用同一提交、同一台空闲机器、相同 `GOMAXPROCS` 和相同电源/温控状态。每个子项至少
采样 10 次：

```bash
GOTOOLCHAIN=go1.25.13 go test ./internal/grid -run '^$' \
  -bench 'Benchmark(Requests|Stream)$' -benchmem -count=10 \
  > /private/tmp/go125-grid.txt
GOTOOLCHAIN=go1.26.6 go test ./internal/grid -run '^$' \
  -bench 'Benchmark(Requests|Stream)$' -benchmem -count=10 \
  > /private/tmp/go126-grid.txt
```

如果环境已安装 `benchstat`，执行：

```bash
benchstat /private/tmp/go125-grid.txt /private/tmp/go126-grid.txt
```

不得为了比较而全局安装未审核工具。原始文件和 `benchstat` 输出应复制到持久化构建制品，
`/private/tmp` 只适合本地临时证据。

### 3.2 2026-08-18 本地执行记录

| 项目 | Go 1.25.13 | Go 1.26.6 | 结论 |
| --- | --- | --- | --- |
| `BenchmarkRequests` | 60 个子项 × 10 次，600 个样本 | 60 个子项 × 10 次，600 个样本 | 数据完整，仅作为开发回归信号 |
| `BenchmarkStream/request/servers=4/par=16` | `context canceled`，退出码 1 | `context canceled`，退出码 1 | 两版本共同基线缺陷，阻断完整 grid 对比 |
| `benchstat` | 环境未安装 | 环境未安装 | 未形成统计性能结论 |

本地临时证据：

- Go 1.25 Requests 和 Stream 失败现场：`/private/tmp/go125-grid.txt`；
- Go 1.26 Requests：`/private/tmp/go126-grid-requests.txt`；
- 聚焦失败：`/private/tmp/go125-grid-focused.txt`、
  `/private/tmp/go126-grid-focused.txt`。

Stream 失败发生在 `internal/grid/benchmark_test.go` 的 `st.Results()`，两种工具链均可重复。
它不是 Go 1.26 单边回归，但在独立修复并重新采集完整样本前，grid 性能门禁保持
**阻断发布**。Task 0 不顺带改变 grid 的 stream 生命周期。

开发 benchmark 不能替代同规格测试环境或生产前压力测试。

## 4. 生产前功能矩阵

下表所有未执行项都保持“阻断发布”。证据必须来自待部署的同一候选二进制。

| 功能 | 命令或工作流 | 证据位置 | 负责人 | 当前结果 |
| --- | --- | --- | --- | --- |
| S3 PUT/GET/list/delete | `mint` workflow；另用 `mc mb/cp/ls/rm` 做候选烟测 | CI artifact/变更单附件 | 发布负责人 | 未执行，阻断 |
| Multipart | `mint` 大对象/multipart 用例 | CI artifact/变更单附件 | 存储测试负责人 | 未执行，阻断 |
| TLS 与证书链 | TLS integration workflow；`mc admin info <tls-alias>` | CI artifact/握手日志 | 安全负责人 | 未执行，阻断 |
| SSE-S3/SSE-KMS | KMS integration workflow 和加密对象读写 | CI artifact/KMS 审计 | 安全负责人 | 未执行，阻断 |
| LDAP | `.github/workflows/iam-integrations.yaml` LDAP job | CI artifact | IAM 负责人 | 未执行，阻断 |
| OIDC | `.github/workflows/iam-integrations.yaml` OIDC job | CI artifact | IAM 负责人 | 未执行，阻断 |
| 内部 grid | `go test ./internal/grid`；修复后重跑完整 grid benchmark | CI artifact/benchmark 文件 | 存储负责人 | benchmark 阻断 |
| Console 登录/浏览/上传 | Console Playwright 流程和 9001 人工烟测 | Playwright artifact/截图 | Console 负责人 | Console 尚未迁入，阻断 |
| 容器 CPU quota | 以相同镜像在限制 CPU 的测试容器中核对 `GOMAXPROCS` 和吞吐 | 容器日志/监控截图 | 平台负责人 | 未执行，阻断 |

功能测试同时覆盖单节点和与生产拓扑一致的多节点环境；外部身份源、KMS 和 TLS 使用测试凭据，
不得把 Secret、Token、私钥或完整 DSN 写入日志或制品。

## 5. 生产性能门禁

在相同主机、磁盘、网络、对象集合、MinIO 配置和并发模型下，分别运行当前 Go 1.24.8
生产二进制与 Go 1.26.6 候选，至少记录：

- PUT、GET、list、delete 和 multipart throughput；
- p50、p95、p99 延迟；
- RSS、CPU、GC CPU、GC pause、heap；
- goroutine、文件描述符和连接数；
- 小对象高并发、大对象持续吞吐、空闲、稳态和压力阶段。

原始压测结果、监控快照、候选 revision、命令参数和数据集摘要必须进入同一变更单。出现统计
显著回归、业务 SLO 超限或资源使用明显恶化时，先用 CPU/heap/trace profile 定位；不得预设
`nogreenteagc`，也不得通过降低并发、缩短时间或更换数据集让结果“通过”。

## 6. 部署与回滚边界

向 `rain@10.0.1.119` 部署、覆盖二进制、修改 systemd 或重启服务前，必须重新取得危险操作
确认。获批后也先执行只读预检，解析实际 service 名称、`ExecStart`、运行用户、二进制路径、
配置路径和当前 revision，禁止凭假设覆盖文件。

部署前必须保存：

1. 当前可执行文件及其校验和；
2. systemd unit 和 drop-in；
3. MinIO 环境配置、证书引用和数据目录参数；
4. `systemctl` 状态、启动时间和最近日志；
5. 旧版本的健康检查结果。

候选启动后至少检查：

- 同一 PID 同时提供 S3/Admin API 端口 9000 和 Console 端口 9001；
- `/minio/health/live` 与 `/minio/health/ready`；
- systemd 未反复重启且日志无 panic、TLS、IAM、KMS 或 storage 初始化错误；
- 第 4 节功能矩阵和第 5 节性能门禁。

任一健康检查失败、错误率异常或 SLO 超限时：停止继续扩散，恢复已备份的旧二进制和原配置，
重启同一个 service，再重复健康检查并保存回滚证据。工具链升级不包含数据格式或数据库 schema
变更，因此不以数据库回滚代替二进制回滚。

