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

### 2.3 2026-08-18 门禁实测记录

在 `5988af90ed17098b050cd8a2e0772d2b4b97ba50` 上执行 2.1 与 2.2 的全部命令，结果如下。

| 门禁 | 结果 | 关键证据 |
| --- | --- | --- |
| 源码策略 | 通过 | `verify-go-toolchain -root .` 退出码 0 |
| 双版本 tidy | 通过 | 1.25.13 与 1.26.6 的 `tidy -diff` 均无输出、退出码 0 |
| Go 1.25.13 最低兼容 | 通过 | `go version` 精确 `go1.25.13`；`vet` 退出码 0；`./cmd` 定向用例 ok；linux amd64/arm64 静态编译成功 |
| Go 1.26.6 主验证 | 通过 | 全量 `go test -tags kqueue,dev ./cmd -count=1` ok 160.4s；`-race` 下 `TestForwarder` 6 个用例与 `./cmd` 定向用例 ok |
| `make verifiers` | 通过 | `golangci-lint` 报告 `0 issues`；`check-gen` 无生成物漂移 |
| 跨平台编译 | 通过 | `cross-compile.sh` 15/15 目标成功 |
| 独立干净检出发布物 | 通过 | 独立 clone 构建 linux/amd64 后严格检查退出码 0，`vcs.revision` 绑定发布提交且 `vcs.modified=false`；`make build-release` 退出码 0 |

环境前置条件缺失，按缺失记录，不计为通过：

- `typos` 二进制未安装，`make lint` 走 Makefile 自带的跳过分支；`.typos.toml` 的检查依赖 CI 执行。
- 在 linked worktree 中执行 2.2 的严格检查会按 2.2 已说明的 VCS stamping 行为报告 `vcs.revision`
  不匹配。该现象由工作区形态引起，不是候选代码缺陷；发布证据必须来自独立干净检出。

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
| S3 PUT/GET/list/delete | `mint` workflow；另用 SigV4 烟测做候选验证 | 见 7.1 部署记录 | 发布负责人 | 部分执行：候选二进制烟测通过；`mint` 套件未执行，仍阻断 |
| Multipart | `mint` 大对象/multipart 用例 | CI artifact/变更单附件 | 存储测试负责人 | 未执行，阻断 |
| TLS 与证书链 | TLS integration workflow；`mc admin info <tls-alias>` | CI artifact/握手日志 | 安全负责人 | 未执行，阻断 |
| SSE-S3/SSE-KMS | KMS integration workflow 和加密对象读写 | CI artifact/KMS 审计 | 安全负责人 | 未执行，阻断 |
| LDAP | `.github/workflows/iam-integrations.yaml` LDAP job | CI artifact | IAM 负责人 | 未执行，阻断 |
| OIDC | `.github/workflows/iam-integrations.yaml` OIDC job | CI artifact | IAM 负责人 | 未执行，阻断 |
| 内部 grid | `go test ./internal/grid`；修复后重跑完整 grid benchmark | CI artifact/benchmark 文件 | 存储负责人 | benchmark 阻断 |
| 转发头与 `aws:SourceIp` | 经上游反向代理访问集群，分别核对直连与内部转发两条路径的审计 `sourceIPAddress`、事件 `Host` 和 `aws:SourceIp` 判定 | 审计日志/策略拒绝记录 | 安全负责人 | 未执行，阻断 |
| Console 登录/浏览/上传 | Console Playwright 流程和 9001 人工烟测 | Playwright artifact/截图 | Console 负责人 | Console 尚未迁入，阻断 |
| 容器 CPU quota | 以相同镜像在限制 CPU 的测试容器中核对 `GOMAXPROCS` 和吞吐 | 容器日志/监控截图 | 平台负责人 | 未执行，阻断 |

功能测试同时覆盖单节点和与生产拓扑一致的多节点环境；外部身份源、KMS 和 TLS 使用测试凭据，
不得把 Secret、Token、私钥或完整 DSN 写入日志或制品。

Forwarder 迁移后删除客户端自报的 `Forwarded`、`X-Forwarded-*` 和 `X-Real-IP`，只从入向连接推导
这些字段。集群前置可信反向代理时，被内部转发的请求在接收节点上看到的来源是该代理地址，而直连
同一节点的请求仍沿用代理写入的 `X-Forwarded-For`。`aws:SourceIp` 策略判定、审计 `sourceIPAddress`
和事件 `Host` 因此可能在两条路径上不一致，上线前必须按真实拓扑确认策略与审计结论符合预期。
该行为是设计选择，不得通过恢复信任客户端 header 规避。

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

## 7. 部署记录

### 7.1 2026-08-19 部署 `71102ab97` 到 10.0.1.119

**目标机实况（部署前只读预检）**：`debian-13-rh`，Linux 6.12.41 x86_64，单节点。服务由**用户级**
systemd 管理（`/home/rain/.config/systemd/user/minio.service`，无 drop-in，`Linger=yes`，
`Type=notify`、`Restart=always`、`SendSIGKILL=no`、`TimeoutStopSec=infinity`），
`EnvironmentFile=/home/rain/.config/minio/minio.env`，
`ExecStart=/home/rain/.local/bin/minio server /home/rain/.local/share/minio/data --address :9000 --console-address :9001`。
本节所述"systemd"均指用户级单元，不存在系统级 minio 单元。

| 项 | 旧 | 新 |
| --- | --- | --- |
| 版本 | `DEVELOPMENT.2026-08-17T05-40-42Z` | `DEVELOPMENT.2026-08-19T01-51-56Z` |
| commit-id | `006e391975058e0b8a25c87c853c1fe25ee764ef` | `71102ab97b44dd98460920da643c83e0be70824e` |
| Runtime | `go1.26.6 linux/amd64` | `go1.26.6 linux/amd64` |
| 大小 / SHA-256 | 120,184,994 / `909f942c…4df4f` | 110,391,458 / `f920ed52…2c52` |

**旧版本已是 `go1.26.6`**：它在 `006e39197`（当时 `go.mod` 仍为 `go 1.24.0`）上被更新的本地工具链
编译，正是本设计第 1 节指出的漂移。因此本次部署**不引入 Go 运行时变更**；运行时差异集中在
`internal/handlers/forwarder.go` 的转发头信任模型，而该路径（`globalForwarder` 的 bucket/DNS 转发与
站点复制、`proxyRequest` 的对等节点代理）在单节点部署上不被走到。

**构建**：`GOTOOLCHAIN=go1.26.6 CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags kqueue -trimpath
--ldflags "$(go run buildscripts/gen-ldflags.go)"`，工作区干净，严格 `verify-go-toolchain`（不带
`-allow-modified`）退出码 0，`vcs.revision` 绑定发布提交且 `vcs.modified=false`。
**注意**：裸 `go build` 不注入 `--ldflags`，二进制会自报 `DEVELOPMENT.GOGET`、丢失版本身份；
发布候选必须带 `gen-ldflags.go` 的输出。

**备份**（第 6 节五项，位于 `/home/rain/minio-deploy-backup/006e39197-20260819T020403Z/`）：
旧二进制及其 `sha256`、unit 文件、`minio.env`、`systemctl show`/`status`、`journalctl` 200 行、
旧版健康基线（`live=200 ready=200`）。旧二进制同时就地保留为 `/home/rain/.local/bin/minio.bak-006e39197`。

**健康检查结果（全部通过）**：版本自报 `commit-id=71102ab97b44…`；`MainPID=644165` 单进程同时提供
9000 与 9001（4 个监听全部属该 PID）；`live=200`、`ready=200`、`cluster=200`；`NRestarts=0`、
`ActiveState=active`；日志无 panic、TLS、IAM、KMS 或 storage 初始化错误。

**S3 烟测（PUT/GET/list/delete，SigV4 直签，凭据不出目标机）**：create bucket 200、PUT 200、
GET 200 且内容逐字节一致、LIST 200 且含该 key、DELETE object 204、DELETE bucket 204，全部 PASS。

**Console 验证**：`:9001` 返回 200，服务的主包 `main.d1b8ec8c.js` 中
`name:"Documentation"`、`name:"License"`、`Visit MinIO Documentation`、`Visit MinIO Blog`、
`label:"Video"`、`"/license"` 六个特征串均为 0。

**仍未执行、仍阻断**：`mint` 套件、multipart、TLS 与证书链、SSE-S3/SSE-KMS、LDAP、OIDC、内部 grid
benchmark、转发头与 `aws:SourceIp` 的真实拓扑验证、容器 CPU quota，以及第 5 节生产性能门禁。
本次部署在这些门禁未通过的情况下经用户明确确认执行，不得据此认为上述项目已满足。

**回滚方式**：`systemctl --user stop minio.service`；
`cp /home/rain/.local/bin/minio.bak-006e39197 /home/rain/.local/bin/minio`；
`systemctl --user start minio.service`；再重复上述健康检查。

