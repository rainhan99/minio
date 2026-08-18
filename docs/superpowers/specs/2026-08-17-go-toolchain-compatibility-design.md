# MinIO Go 1.25/1.26 工具链兼容升级设计

- 日期：2026-08-17
- 状态：实施中；源码改造已完成，独立干净检出发布物与部署门禁待执行
- 当前根模块：`go 1.25.0`、`toolchain go1.26.6`
- 待迁入 Console 上游基线：`go 1.24.0`、`toolchain go1.24.4`，迁入时必须对齐根模块策略
- 目标最低兼容版本：Go 1.25.13
- 目标生产构建版本：Go 1.26.6

## 1. 背景

升级前 MinIO 根模块、最终 Console 模块和 11 个 GitHub Actions 工作流固定在 Go 1.24，
但 `buildscripts/checkdeps.sh` 仍声称 Go 1.16 即可构建。Go 官方只向最近两个大版本提供
安全修复；Go 1.26 已在 2026-02-10 发布，因此截至本设计日期受支持的版本是 Go 1.25
和 Go 1.26，Go 1.24 已退出安全维护窗口。

升级前的 `toolchain go1.24.8` 不是精确版本锁。`GOTOOLCHAIN=auto` 下，如果本机 Go 比
`toolchain` 行更新，Go 命令会直接使用本机更新版本。本地使用 Go 1.26.6 运行现有测试时，
`cmd.TestTrackingResponseWriter` 因 `httptest.ResponseRecorder` 拒绝为 1xx 状态写响应体而
失败；同一测试在 Go 1.24.8 下通过。这证明现有仓库已经会被较新本地工具链执行，但没有
对应兼容保证。

本次升级必须在 Console 源码迁入前独立完成，避免把 Go 版本、最终 Console 恢复和 IAM
功能三类问题混在同一变更面。

官方依据：

- Go 发布和支持政策：https://go.dev/doc/devel/release
- Go 工具链选择语义：https://go.dev/doc/toolchain
- Go 1.26 发布说明：https://go.dev/doc/go1.26
- GODEBUG 兼容行为：https://go.dev/doc/godebug

## 2. 目标

1. 生产发布物由精确的 Go 1.26.6 构建并携带当前标准库安全修复。
2. 源码保持 Go 1.25.13 最低构建兼容，覆盖 Go 官方当前两个受支持大版本。
3. 根模块和迁入后的 `console/` 子模块采用相同版本策略。
4. CI、开发检查、发布构建和文档使用一致的版本来源，不再出现 1.16/1.24/1.26 并存。
5. 修复已确认的 Go 1.26 HTTP 测试语义问题，不修改生产 `trackingResponseWriter` 的透传
   行为。
6. 将 `httputil.ReverseProxy.Director` 迁移为安全的 `Rewrite`，保护代理添加的请求字段
   不被恶意 hop-by-hop header 删除。
7. 对 Go 1.25/1.26 的 TLS、URL、容器 CPU、GC、跨平台和静态构建行为建立回归证据。
8. 消除测试对公共 DNS 的非确定性依赖，使双版本 CI 的失败可归因。

## 3. 非目标

- 不在本次升级中使用 Go 1.26 新语言语法；
- 不把最低语言版本直接提升到 Go 1.26；
- 不启用实验性的 `runtime/secret` 或其他 `GOEXPERIMENT` 功能；
- 不因为 Go 1.26 提供 `http.CrossOriginProtection` 就替换已批准的 Console CSRF 设计；
- 不顺带升级全部第三方 Go 依赖；只有确认阻塞 Go 1.26 的依赖才单独评估；
- 不为通过测试而设置全局兼容 `GODEBUG` 掩盖真实协议错误；
- 不在本任务中部署到 `10.0.1.119`。

## 4. 方案比较

### 4.1 采用：上一受支持版本为最低版本，最新版本为生产工具链

根模块最终设置：

```text
go 1.25.0
toolchain go1.26.6
```

Go 1.25.13 使用 `GOTOOLCHAIN=local` 验证最低兼容；Go 1.26.6 使用
`GOTOOLCHAIN=local` 执行完整验证和发布构建。`console/` 迁入后使用同一策略。

优点：

- 与 Go 官方两个受支持版本窗口一致；
- 生产使用最新安全补丁；
- 不强迫所有开发环境立即放弃 Go 1.25；
- `go` 指令保持 1.25，可分离 Go 1.25 和 Go 1.26 的默认行为切换；
- 2027 年 Go 1.27 发布后可机械滚动为最低 1.26、生产 1.27。

代价：CI 需要维护两个版本的兼容 job，Go 1.25 与 1.26 差异必须有真实测试覆盖。

### 4.2 未采用：只支持 Go 1.26

直接设置 `go 1.26.0`、`toolchain go1.26.6` 最简单，但会同时放弃仍受官方支持的 Go 1.25，
并一次性启用 Go 1.26 的 URL、crypto random 和新增 PQ TLS 默认语义。对停止跟踪上游、需要
自主维护的存储服务而言，变更面过大且回归定位困难。

### 4.3 未采用：保留 `go 1.24.0`，只用 Go 1.26 编译

该方案能获得 Go 1.26 标准库修复并保留较多旧默认值，但仍公开宣称已经退出维护的 Go 1.24
可作为最低构建版本，也无法解决 CI 和 `checkdeps.sh` 的版本策略矛盾。

## 5. 版本与构建策略

### 5.1 模块版本

- 根 `go.mod`：`go 1.25.0`、`toolchain go1.26.6`；
- `console/go.mod`：迁入后同步为 `go 1.25.0`、`toolchain go1.26.6`；
- `buildscripts/checkdeps.sh`：最低版本改为 1.25.0；
- 所有现有 Go CI workflow 从 `1.24.x` 改到精确的 `1.26.6`；
- 新增独立兼容 job 使用精确的 `1.25.13`，并设置 `GOTOOLCHAIN=local` 防止自动切换；
- Go 1.26 job 同样设置 `GOTOOLCHAIN=local`，保证 CI 实际版本等于 setup-go 安装版本。

### 5.2 发布可复现性

`toolchain` 行只提供建议的最低工具链，不能限制更高的本地 Go。正式发布目标必须显式使用
`GOTOOLCHAIN=go1.26.6`，并在构建后用 `go version -m` 验证：

- 编译器为 `go1.26.6`；
- `CGO_ENABLED=0`；
- 目标 OS/ARCH 正确；
- VCS revision 与发布提交一致；
- `vcs.modified=false`。

普通开发测试可以使用 1.25.13 或 1.26.6；使用更高的预览/未来版本只作为 allowed-failure
前向兼容信号，不能生成生产发布物。

## 6. 标准库行为边界

### 6.1 Go 1.25 `go` 指令带来的默认变化

将 `go` 指令从 1.24 提升到 1.25 会启用：

- Linux 容器 cgroup CPU quota 参与默认 `GOMAXPROCS`；
- 运行时周期性更新 `GOMAXPROCS`；
- TLS 1.2 SHA-1 签名算法默认禁用；
- 新建证书缺失 SubjectKeyId 时使用 SHA-256；
- Linux 内存映射增加运行时用途标注。

这些行为不通过永久 `GODEBUG` 回退。升级验证必须覆盖容器 CPU、TLS/KMS/LDAP/OIDC、
证书加载和运行时资源指标；只有真实兼容障碍才允许提出带移除期限的临时回退。

### 6.2 Go 1.26 工具链变化

Go 1.26 默认启用 Green Tea GC 和 64 位堆基址随机化。性能验收对比 Go 1.25.13 与
Go 1.26.6 的吞吐、p95/p99 延迟、RSS、GC CPU、暂停时间和 goroutine 数，不预设
`GOEXPERIMENT=nogreenteagc`。

因为 module `go` 指令保持 1.25，Go 1.26 的以下兼容行为继续使用 pre-1.26 默认值：

- `urlstrictcolons=0`；
- `cryptocustomrand=1`；
- `tlssecpmlkem=0`。

验证必须读取发布二进制 `go version -m` 中的 `DefaultGODEBUG` 证明该边界，不能依赖人工
记忆。未来把 `go` 指令提升到 1.26 时，再单独评估并启用这些语义。

MinIO 已显式设置 `crypto.TLSCurveIDs()`，因此 Go 1.26 新增默认曲线不会自动覆盖 MinIO
主动配置，但必须用真实 TLS client/server 握手测试证明 S3、Console、内部 grid、etcd、KMS
和外部身份源没有退化。

## 7. 已确认代码改造

### 7.1 合法 HTTP 测试

`cmd.TestTrackingResponseWriter` 当前用状态码 123 后写 body。HTTP 1xx 不允许 body，Go 1.26
的 `httptest.ResponseRecorder.Write` 正确返回 `http.ErrBodyNotAllowed`。

测试改为合法、允许 body 的成功状态码，并继续验证：

- `WriteHeader` 标记 `headerWritten`；
- `Write` 透传到底层 writer；
- status 和 body 被正确记录；
- `Unwrap` 返回原 writer。

只修改测试 fixture，不吞掉 `trackingResponseWriter.Write` 的底层错误。`WriteHeader` 只验证
header 状态的其他测试可以继续使用非标准三位状态码，但优先统一为合法状态以表达意图。

### 7.2 ReverseProxy 安全迁移

`internal/handlers/forwarder.go` 从 `Director` 迁移到 `Rewrite`。测试先覆盖：

- 原请求 scheme、host、path、raw query 和必要 header 正确传递；
- 代理明确设置的 `X-Forwarded-*` 不被客户端 `Connection`/hop-by-hop header 删除；
- 客户端伪造的 forwarding headers 不获得信任；
- websocket/upgrade、context cancel、自定义 transport 和 error handler 保持现有行为；
- query 参数净化语义不漂移。

实现使用 `ProxyRequest.SetURL`/`SetXForwarded` 或等价的显式字段复制；禁止同时设置
`Director` 与 `Rewrite`。迁移只改变代理构造边界，不重写 `Forwarder` 的 buffer pool、transport
和错误处理职责。

### 7.3 DNS 确定性

升级前 `cmd.TestCreateEndpoints` 使用 `example.com`/`example.org` 并可能触发公共 DNS 查询。
实现使用 RFC 5737 文档地址表达远端节点，并显式断言这些 endpoint 的 `IsLocal == false`；
没有为测试引入新的生产 resolver seam，也没有修改开发机或 CI 的系统 hosts。

生产 resolver 行为不改变。测试证明本地/远端 endpoint 分类与错误映射，不测试公共 DNS
是否在线。

## 8. CI 验证矩阵

### 8.1 Go 1.25.13 兼容 job

- `GOTOOLCHAIN=local go version` 必须输出 1.25.13；
- `go mod tidy -diff`；
- 根模块 build；
- 根模块关键 unit tests；
- Console 迁入后执行 Console Go tests；
- Linux amd64/arm64 `CGO_ENABLED=0` compile；
- 不生成正式发布物。

### 8.2 Go 1.26.6 主 job

- 完整 `make verifiers`；
- 完整 Go tests 和相关 race tests；
- Linux amd64/arm64、Darwin arm64、Windows amd64、FreeBSD amd64 编译；
- TLS、proxy、URL、endpoint 和 Console IAM 安全回归；
- 生成唯一正式 `minio` 发布物；
- `go version -m` 元数据断言。

### 8.3 环境隔离

- 网络契约测试与 hermetic unit tests 分离；
- unit tests 禁止依赖公共 DNS；
- 需要 Docker/PostgreSQL/LDAP/OIDC/KMS 的 job 显式声明服务；
- 工具链下载在 job setup 完成，测试阶段不得隐式切换版本；
- cache key 包含精确 Go 版本、OS、ARCH 和 `go.sum` 摘要。

## 9. 性能与上线策略

Go 1.26.6 发布候选与当前 Go 1.24.8 生产构建使用相同 MinIO 配置和数据集比较：

- 单对象 PUT/GET、multipart、list 和删除；
- Console 登录、Bucket 浏览和对象上传；
- 加密/KMS 与 TLS；
- 小对象高并发和大对象持续吞吐；
- idle、稳态和压力下的 RSS/GC/CPU/FD/goroutine。

功能结果必须一致。性能不设置“一律更快”的假设；若 p99、吞吐或 RSS 出现显著回归，先用
profile 定位，再决定修复、临时 GC opt-out 或暂缓生产升级。

生产仍按单二进制、单 systemd 服务部署。先在测试环境运行 1.26.6 发布物，再在
`10.0.1.119` 备份旧二进制后滚动替换。部署和重启仍需要独立危险操作确认。

## 10. 实施顺序

1. 为工具链版本检查和二进制元数据写失败验证；
2. 修复合法 HTTP 测试 fixture；
3. 将公共 DNS 测试改为确定性 resolver seam；
4. 为 ReverseProxy 写安全回归测试并迁移到 `Rewrite`；
5. 提升根模块最低版本与推荐工具链；
6. 更新 `checkdeps.sh` 和全部 Go CI；
7. 运行 Go 1.25.13/1.26.6 双版本矩阵和跨平台构建；
8. 记录性能基线和发布元数据；
9. 迁入 Console 后同步其 module 版本并重跑矩阵；
10. Task 0 验收后再开始原计划 Task 1。

## 11. 验收标准

截至 2026-08-18 的实施记录：源码策略检查器已经接受当前仓库配置；开发侧
`BenchmarkRequests` 已取得 Go 1.25.13/1.26.6 各 60 个子项、每项 10 次的完整样本；
`BenchmarkStream/request/servers=4/par=16` 在两个工具链下均因 `context canceled` 失败，
因此完整 grid 性能对比保持阻断。独立干净检出的发布物元数据、外部 IAM/KMS/TLS、Console、
生产性能和部署验证尚未完成，不计为通过。

1. `go.mod` 使用 `go 1.25.0`、`toolchain go1.26.6`。
2. 迁入后的 `console/go.mod` 使用相同策略。
3. `buildscripts/checkdeps.sh` 与模块最低版本一致。
4. 所有正式 CI 使用精确 Go 1.26.6，兼容 job 使用精确 Go 1.25.13，且均禁止隐式切换。
5. Go 1.25.13 和 1.26.6 的根构建、关键测试、vet 和静态跨平台构建通过。
6. Go 1.26.6 完整 test/race/verifier 通过，任何环境依赖失败有独立 job 和明确前置条件。
7. `TestTrackingResponseWriter` 使用合法 HTTP 语义并在两个版本通过。
8. endpoint 单元测试不访问公共 DNS。
9. Forwarder 使用 `Rewrite`，伪造/hop-by-hop header 安全测试通过。
10. 发布 Linux amd64/arm64 二进制为 `CGO_ENABLED=0`、Go 1.26.6、正确 revision 且工作树未修改。
11. TLS、URL、KMS、身份源、S3、Console 和对象操作无行为回归。
12. 性能对比有实际数据；若存在显著回归，生产部署前必须形成明确处置结论。

## 12. 回滚

- 代码回滚恢复上一版 module、CI 和 proxy 实现；
- 生产回滚只恢复上一版 `minio` 二进制并重启同一个 service；
- 工具链升级不包含数据格式或数据库 schema 变更；
- 不通过生产环境永久设置兼容 `GODEBUG` 代替代码回滚；
- 已生成的 Go 1.26 测试二进制不覆盖已备份的生产二进制。
