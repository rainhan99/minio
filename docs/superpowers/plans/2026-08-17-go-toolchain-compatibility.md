# MinIO Go Toolchain Compatibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 MinIO 的最低源码兼容版本提升到 Go 1.25.13，并固定 Go 1.26.6 作为生产发布工具链，同时修复已确认的 HTTP、DNS 与反向代理兼容问题，建立可执行的双版本及发布元数据门禁。

**Architecture:** `go 1.25.0` 定义源码与标准库兼容边界，`toolchain go1.26.6` 定义推荐开发工具链；CI 使用 `GOTOOLCHAIN=local` 分别锁定 1.25.13 和 1.26.6，正式发布使用显式的 Go 1.26.6 构建入口。工具链策略由仓库内的验证命令统一检查，生产协议行为通过现有包级测试和新增安全回归测试保护，不引入全局 `GODEBUG` 回退。

**Tech Stack:** Go 1.25.13、Go 1.26.6、`net/http/httputil.ReverseProxy.Rewrite`、`runtime/debug`/`debug/buildinfo`、GitHub Actions、Docker、Go `testing`。

**Design:** `docs/superpowers/specs/2026-08-17-go-toolchain-compatibility-design.md`

## Global Constraints

1. 本计划是 Console IAM 实施计划的前置 Task 0；Task 0 验收前不得开始迁入 `console/`。
2. 当前会话没有获得并行子代理授权，执行时使用 `superpowers:executing-plans` 串行推进，并始终只保持一个任务为进行中。
3. 每项代码变更严格执行 RED → GREEN → REFACTOR；已在调研中观察到的失败也必须在实施时重新运行并保存命令输出。
4. 不升级第三方依赖；若 Go 1.26 暴露依赖不兼容，立即停止该任务并单独评估，不用批量 `go get -u` 绕过。
5. 不设置永久的 `GODEBUG`、`GOEXPERIMENT=nogreenteagc` 或宽泛的测试跳过规则。
6. `ReverseProxy` 迁移必须同时保护正常 HTTP、恶意 hop-by-hop header、伪造 forwarding header、Upgrade、context 和错误处理路径，不能只替换字段名。
7. 单元测试不得访问公共 DNS；需要表达远端节点时使用 RFC 5737 文档地址。
8. 普通开发构建继续使用当前显式工具链；只有 `build-release` 和正式发布流水线强制 Go 1.26.6，避免破坏 Go 1.25.13 兼容验证。
9. `console/go.mod` 尚不存在；其版本对齐放在原 Console 计划 Task 1 的强制校验中，不在本计划创建空目录或伪模块。
10. 每个提交命令只是建议检查点。执行 `git commit`、`git push`、数据库变更、systemd 修改或部署前必须再次取得明确确认。
11. Task 0 不部署到 `rain@10.0.1.119`，也不修改该主机配置。

---

## Task 0.1：建立可执行的工具链策略检查器

**Files:**

- Create: `buildscripts/verify-go-toolchain/main.go`
- Create: `buildscripts/verify-go-toolchain/main_test.go`

**Contract:**

```go
const (
	minimumGoDirective   = "1.25.0"
	compatibilityVersion = "1.25.13"
	releaseToolchain     = "go1.26.6"
)

type options struct {
	root          string
	binary        string
	revision      string
	goos          string
	goarch        string
	allowModified bool
}

func verifySourcePolicy(root string) error
func verifyBuildInfo(info *debug.BuildInfo, opts options) error
func run(opts options) error
```

`verifySourcePolicy` 必须检查：

- 根 `go.mod` 的 `go` 与 `toolchain` 指令；
- `buildscripts/checkdeps.sh` 的最低版本；
- 所有含 `actions/setup-go` 的 workflow 都声明 `GOTOOLCHAIN: local`；
- 主 workflow 只使用精确 `1.26.6`，兼容 workflow 只使用精确 `1.25.13`；
- Go builder Dockerfile 不再含 `golang:1.24`；
- `Makefile` 的正式发布入口显式选择 `go1.26.6`。

`verifyBuildInfo` 必须检查：

- `GoVersion == "go1.26.6"`；
- `CGO_ENABLED=0`；
- `GOOS`、`GOARCH` 与传入值一致；
- `vcs.revision` 与传入 revision 一致；
- 正式模式要求 `vcs.modified=false`；
- `DefaultGODEBUG` 至少包含 Go 1.25 已启用、Go 1.26 暂缓的边界：
  `containermaxprocs=1`、`updatemaxprocs=1`、`tlssha1=0`、
  `x509sha256skid=1`、`urlstrictcolons=0`、`tlssecpmlkem=0`。

### Steps

- [ ] **Step 1：为源码策略写失败测试**

  使用 `t.TempDir()` 创建最小仓库 fixture，按表驱动覆盖：正确策略、错误 `go` 指令、错误
  `toolchain`、旧 `checkdeps`、漂移的 workflow、缺失 `GOTOOLCHAIN=local`、旧 Docker builder 和
  缺失发布入口。

  ```go
  func TestVerifySourcePolicy(t *testing.T) {
	  tests := []struct {
		  name    string
		  mutate  func(string)
		  wantErr string
	  }{
		  {name: "valid"},
		  {name: "rejects old go directive", mutate: replace("go 1.25.0", "go 1.24.0"), wantErr: "go directive"},
		  {name: "rejects floating workflow version", mutate: replace("1.26.6", "1.26.x"), wantErr: "exact Go version"},
	  }
	  // 每个 case 都调用 verifySourcePolicy，而不是检查实现源码字符串。
  }
  ```

  Run:

  ```bash
  GOTOOLCHAIN=go1.26.6 go test ./buildscripts/verify-go-toolchain -run TestVerifySourcePolicy -count=1
  ```

  Expected: FAIL，`verifySourcePolicy` 尚不存在。

- [ ] **Step 2：为发布二进制元数据写失败测试**

  构造 `debug.BuildInfo` fixture，覆盖错误编译器、CGO、目标平台、revision、dirty 标记和
  `DefaultGODEBUG`。`allowModified` 只允许本地未提交验证使用，默认值必须保持 `false`。

  Run:

  ```bash
  GOTOOLCHAIN=go1.26.6 go test ./buildscripts/verify-go-toolchain -run TestVerifyBuildInfo -count=1
  ```

  Expected: FAIL，`verifyBuildInfo` 尚不存在。

- [ ] **Step 3：实现最小检查命令**

  使用标准库 `flag`、`os`、`path/filepath`、`runtime/debug`、`debug/buildinfo`；解析
  `go.mod` 可使用仓库已有的 `golang.org/x/mod/modfile`，不得增加依赖。错误必须一次列出全部
  漂移项，便于 CI 定位。

  命令接口：

  ```bash
  go run ./buildscripts/verify-go-toolchain -root .
  go run ./buildscripts/verify-go-toolchain \
    -root . -binary ./minio -revision "$(git rev-parse HEAD)" \
    -goos linux -goarch amd64
  ```

- [ ] **Step 4：验证测试变绿，并证明当前仓库策略会被拒绝**

  Run:

  ```bash
  GOTOOLCHAIN=go1.26.6 go test ./buildscripts/verify-go-toolchain -count=1
  GOTOOLCHAIN=go1.26.6 go run ./buildscripts/verify-go-toolchain -root .
  ```

  Expected: 单元测试 PASS；第二条 FAIL，并明确报告当前 `go 1.24.0`、`go1.24.8`、
  `GO_VERSION=1.16`、旧 workflow/Dockerfile 和缺失发布入口。

- [ ] **Step 5：重构检查器，保持单一职责**

  文件读取、规则判断、build info 判断分别为小函数；测试 helper 只负责 fixture，不把生产规则
  复制到测试中。

**Suggested checkpoint:** `test: add Go toolchain policy verifier`

---

## Task 0.2：修复 Go 1.26 下非法的 HTTP 测试 fixture

**Files:**

- Modify: `cmd/api-response_test.go`

### Steps

- [ ] **Step 1：重现 Go 1.26 红灯**

  Run:

  ```bash
  GOTOOLCHAIN=go1.26.6 go test -tags kqueue,dev ./cmd -run '^TestTrackingResponseWriter$' -count=1
  ```

  Expected: FAIL，错误为 `http: request method or response status code does not allow body`。

- [ ] **Step 2：证明旧工具链结果差异**

  Run:

  ```bash
  GOTOOLCHAIN=go1.24.8 go test -tags kqueue,dev ./cmd -run '^TestTrackingResponseWriter$' -count=1
  ```

  Expected: PASS；该结果仅作为兼容性根因证据，不再把 Go 1.24 作为支持版本。

- [ ] **Step 3：只修正测试输入**

  将会写 body 的 `123` 改为 `http.StatusCreated`，断言同步为 201。只验证 header 状态的两个
  测试也统一使用合法的 `http.StatusNoContent`；不得修改 `trackingResponseWriter.Write`，不得
  忽略底层 writer 错误。

  ```go
  trw.WriteHeader(http.StatusCreated)
  // ...
  if resp.StatusCode != http.StatusCreated {
	  t.Fatalf("unexpected status: %v", resp.StatusCode)
  }
  ```

- [ ] **Step 4：双版本验证**

  Run:

  ```bash
  GOTOOLCHAIN=go1.25.13 go test -tags kqueue,dev ./cmd -run '^Test(TrackingResponseWriter|HeadersAlreadyWritten|HeadersAlreadyWrittenWrapped)$' -count=1
  GOTOOLCHAIN=go1.26.6 go test -tags kqueue,dev ./cmd -run '^Test(TrackingResponseWriter|HeadersAlreadyWritten|HeadersAlreadyWrittenWrapped)$' -count=1
  ```

  Expected: 两个版本均 PASS。

**Suggested checkpoint:** `test: use legal HTTP status in response writer tests`

---

## Task 0.3：消除 endpoint 单元测试的公共 DNS 依赖

**Files:**

- Modify: `cmd/endpoint_test.go`

### Steps

- [ ] **Step 1：记录现有非确定性失败**

  Run:

  ```bash
  GOTOOLCHAIN=go1.26.6 go test -tags kqueue,dev ./cmd -run '^TestCreateEndpoints$' -count=20
  ```

  Expected: 当前环境可能出现 `lookup example.com`/`example.org` 超时；即使本轮偶然 PASS，测试
  数据仍明确依赖公共 DNS，因此继续修复。

- [ ] **Step 2：用 RFC 5737 地址表达远端节点**

  在 `TestCreateEndpoints` 内定义可读的测试常量：

  ```go
  const (
	  remoteEndpointA = "192.0.2.10"
	  remoteEndpointB = "198.51.100.20"
	  remoteEndpointC = "203.0.113.30"
  )
  ```

  只替换 `TestCreateEndpoints` 中用于远端分类的 `example.com`/`example.org`/`example.net`；需要
  表达“同主机不同路径”的 case 必须重复使用同一个文档 IP，不能因替换而改变冲突语义。
  其他仅测试 URL 解析、peer 字符串排序且不触发解析的用例保持原样，控制变更面。

- [ ] **Step 3：加强分类断言**

  保留全部 `Endpoint.String()` 断言，并显式断言文档 IP endpoint 的 `IsLocal == false`。不要
  新增 resolver 抽象：当前问题只存在于测试 fixture，引入生产 seam 会扩大接口面且没有生产
  需求。

- [ ] **Step 4：重复及 race 验证**

  Run:

  ```bash
  GOTOOLCHAIN=go1.25.13 go test -tags kqueue,dev ./cmd -run '^TestCreateEndpoints$' -count=20
  GOTOOLCHAIN=go1.26.6 go test -tags kqueue,dev ./cmd -run '^TestCreateEndpoints$' -count=20
  GOTOOLCHAIN=go1.26.6 go test -race -tags kqueue,dev ./cmd -run '^TestCreateEndpoints$' -count=1
  ```

  Expected: 全部 PASS，输出不含公共域名解析错误。

**Suggested checkpoint:** `test: remove public DNS dependency from endpoint cases`

---

## Task 0.4：将 Forwarder 从 Director 安全迁移到 Rewrite

**Files:**

- Create: `internal/handlers/forwarder_test.go`
- Modify: `internal/handlers/forwarder.go`

### Behavioral contract

迁移后 outbound request 必须满足：

- URL scheme/host/path/raw path/raw query 与迁移前正常请求一致；
- `PassHost=false` 使用 target host，`PassHost=true` 保留原 Host；
- `X-Real-IP`、`X-Forwarded-Proto`、`X-Forwarded-Port`、`X-Forwarded-Host` 只从当前
  `ProxyRequest.In` 的连接、TLS 和 Host 推导；
- 删除客户端传入的 `Forwarded` 和所有 `X-Forwarded-*`/`X-Real-IP`，不信任伪造值；
- hop-by-hop header 清理发生在代理字段写入之前，客户端不能用 `Connection:` 删除代理结果；
- 保留标准 `ReverseProxy` 已有的 `X-Forwarded-For` 输出，但只记录当前连接地址，不再拼接客户端
  自报的 forwarding 链；
- GET 的 background-context 行为、非 GET 的取消传播、自定义 transport/error handler、Upgrade
  和 buffer/flush 设置保持现状。

### Steps

- [ ] **Step 1：建立 transport 捕获 helper 和正常路径特征测试**

  ```go
  type roundTripFunc func(*http.Request) (*http.Response, error)

  func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	  return f(req)
  }
  ```

  使用真实 `Forwarder.ServeHTTP` 和捕获 transport，先覆盖：URL、raw query、Host、普通 header、
  GET/POST context、Upgrade 和自定义 ErrorHandler。这些特征测试在当前 Director 实现上必须
  PASS；如果失败，先修正测试对现有语义的理解，不修改生产代码。

  Run:

  ```bash
  GOTOOLCHAIN=go1.26.6 go test ./internal/handlers -run '^TestForwarder(Characterization|ErrorHandler|Context|Upgrade)$' -count=1
  ```

- [ ] **Step 2：写 hop-by-hop 与伪造 header 的红灯测试**

  构造请求：

  ```go
  req.Header.Set("Connection", "X-Forwarded-Host, X-Real-IP")
  req.Header.Set("X-Forwarded-Host", "attacker.invalid")
  req.Header.Set("X-Real-IP", "203.0.113.66")
  req.Header.Set("X-Forwarded-Proto", "https")
  req.RemoteAddr = "192.0.2.44:43210"
  req.Host = "storage.internal:9000"
  ```

  断言 backend 看到 `X-Real-IP=192.0.2.44`、`X-Forwarded-For=192.0.2.44`、
  `X-Forwarded-Proto=http`、`X-Forwarded-Port=9000`、
  `X-Forwarded-Host=storage.internal:9000`，且不含攻击者的值。

  Run:

  ```bash
  GOTOOLCHAIN=go1.26.6 go test ./internal/handlers -run '^TestForwarderRejectsSpoofedForwardingHeaders$' -count=1
  ```

  Expected: FAIL；当前 Director 在 hop-by-hop 清理前写 header，并保留客户端伪造字段。

- [ ] **Step 3：实现 Rewrite 边界**

  `ServeHTTP` 只设置 `Rewrite`，禁止同时设置 `Director`：

  ```go
  Rewrite: func(proxyReq *httputil.ProxyRequest) {
	  f.modifyRequest(proxyReq.Out, inReq.URL)
	  f.rewriter.Rewrite(proxyReq)
  },
  ```

  将 `modifyRequest` 收敛为 URL、Host、HTTP 版本和 context 处理；将 forwarding header 的唯一
  写入口改为：

  ```go
  func (rw *headerRewriter) Rewrite(proxyReq *httputil.ProxyRequest) {
	  outReq, inReq := proxyReq.Out, proxyReq.In
	  for _, name := range []string{
		  "Forwarded", xForwardedFor, xForwardedHost,
		  xForwardedPort, xForwardedProto, xRealIP,
	  } {
		  outReq.Header.Del(name)
	  }
	  proxyReq.SetXForwarded()
	  // 再仅从 inReq.RemoteAddr 设置 X-Real-IP，并从 inReq 设置 X-Forwarded-Port。
  }
  ```

  `SetXForwarded` 恢复 Director 模式原本自动生成的 `X-Forwarded-For`，但输入来自
  `ProxyRequest.In` 的真实连接元数据；不得复制客户端 forwarding header。

- [ ] **Step 4：验证正常与攻击路径全部变绿**

  Run:

  ```bash
  GOTOOLCHAIN=go1.25.13 go test ./internal/handlers -run '^TestForwarder' -count=1
  GOTOOLCHAIN=go1.26.6 go test ./internal/handlers -run '^TestForwarder' -count=1
  GOTOOLCHAIN=go1.26.6 go test -race ./internal/handlers -run '^TestForwarder' -count=1
  ```

  Expected: 全部 PASS。

- [ ] **Step 5：运行调用方包回归**

  Run:

  ```bash
  GOTOOLCHAIN=go1.26.6 go test -tags kqueue,dev ./cmd -run 'Test.*(Proxy|Forward|Handler)' -count=1
  ```

  Expected: PASS；若 pattern 未匹配测试，改为执行 `go test -tags kqueue,dev ./cmd -count=1`，不得
  用“无测试可运行”充当验证。

**Suggested checkpoint:** `fix(proxy): migrate Forwarder to secure Rewrite hook`

---

## Task 0.5：升级模块、依赖检查和正式构建入口

**Files:**

- Modify: `go.mod`
- Modify: `go.sum`（仅当 `go mod tidy` 产生必要变化）
- Modify: `buildscripts/checkdeps.sh`
- Modify: `Makefile`
- Modify: `Dockerfile.release`
- Modify: `Dockerfile.release.old_cpu`
- Modify: `Dockerfile.hotfix`

### Steps

- [ ] **Step 1：确认策略检查器仍为红灯**

  Run:

  ```bash
  GOTOOLCHAIN=go1.26.6 go run ./buildscripts/verify-go-toolchain -root .
  ```

  Expected: FAIL，至少报告模块、`checkdeps.sh`、Docker builder、Makefile 和 workflow 漂移。

- [ ] **Step 2：只更新模块指令，不升级依赖**

  Run:

  ```bash
  GOTOOLCHAIN=go1.26.6 go mod edit -go=1.25.0 -toolchain=go1.26.6
  GOTOOLCHAIN=go1.26.6 go mod tidy -compat=1.21 -diff
  ```

  Expected: `go.mod` 顶部为：

  ```text
  go 1.25.0

  toolchain go1.26.6
  ```

  `tidy -diff` 不应包含依赖版本升级；若有变化，先解释原因再决定是否应用。

- [ ] **Step 3：同步最低依赖检查**

  将 `GO_VERSION="1.16"` 改为 `GO_VERSION="1.25.0"`，并为版本比较函数补充 shell 行为验证：

  ```bash
  GOTOOLCHAIN=go1.25.13 bash buildscripts/checkdeps.sh
  GOTOOLCHAIN=go1.26.6 bash buildscripts/checkdeps.sh
  ```

  Expected: 两个受支持版本均 PASS。

- [ ] **Step 4：增加正式发布构建入口**

  保持现有 `build` 使用调用者工具链，新增：

  ```make
  RELEASE_GO_TOOLCHAIN ?= go1.26.6

  build-release: ## builds a release binary with the pinned Go toolchain
	@test -z "$$(git status --porcelain)" || \
		(echo "release build requires a clean worktree"; false)
	@env GOTOOLCHAIN=$(RELEASE_GO_TOOLCHAIN) $(MAKE) build
	@env GOTOOLCHAIN=$(RELEASE_GO_TOOLCHAIN) go run ./buildscripts/verify-go-toolchain \
		-root . -binary $(PWD)/minio -revision "$$(git rev-parse HEAD)" \
		-goos $(GOOS) -goarch $(GOARCH)
  ```

  nested `make` 继承精确 `GOTOOLCHAIN`，使 `checkdeps.sh`、debug helper、ldflags 和最终 MinIO
  二进制都使用 Go 1.26.6。开发阶段因工作树未提交，不运行该正式 target；使用 Task 0.7 的手工
  构建加 `-allow-modified` 验证。正式 target 永远保持 clean-worktree 严格检查。

- [ ] **Step 5：更新 Go builder 镜像**

  三个 Dockerfile 的 builder 改为精确 `golang:1.26.6-alpine`。这些 stage 当前只编译
  `minisign`，仍纳入策略以避免仓库出现第三套已停止支持的 Go 版本。不要顺带更新 UBI base、
  `minisign` 或 curl。

- [ ] **Step 6：局部验证**

  Run:

  ```bash
  GOTOOLCHAIN=go1.25.13 go test ./buildscripts/verify-go-toolchain ./internal/handlers
  GOTOOLCHAIN=go1.26.6 go test ./buildscripts/verify-go-toolchain ./internal/handlers
  ```

  Expected: 单元测试 PASS；完整 source-policy 检查仍只因尚未更新的 workflow 而 FAIL。

**Suggested checkpoint:** `build: adopt Go 1.25 compatibility and Go 1.26 release toolchain`

---

## Task 0.6：锁定 CI 的双版本矩阵

**Files:**

- Create: `.github/workflows/go-compat.yml`
- Modify: `.github/workflows/iam-integrations.yaml`
- Modify: `.github/workflows/go.yml`
- Modify: `.github/workflows/mint.yml`
- Modify: `.github/workflows/go-lint.yml`
- Modify: `.github/workflows/go-healing.yml`
- Modify: `.github/workflows/upgrade-ci-cd.yaml`
- Modify: `.github/workflows/root-disable.yml`
- Modify: `.github/workflows/vulncheck.yml`
- Modify: `.github/workflows/go-resiliency.yml`
- Modify: `.github/workflows/replication.yaml`
- Modify: `.github/workflows/go-cross.yml`

### Steps

- [ ] **Step 1：创建最低版本兼容 workflow**

  `.github/workflows/go-compat.yml` 使用独立 job：

  ```yaml
  name: Go compatibility

  on:
    pull_request:
      branches: [master]

  permissions:
    contents: read

  env:
    GOTOOLCHAIN: local

  jobs:
    minimum-supported:
      runs-on: ubuntu-latest
      steps:
        - uses: actions/checkout@v4
        - uses: actions/setup-go@v5
          with:
            go-version: 1.25.13
            check-latest: false
        - name: Verify exact toolchain
          run: test "$(go env GOVERSION)" = "go1.25.13"
        - name: Verify module and focused compatibility suite
          run: |
            go mod tidy -compat=1.21 -diff
            go test ./buildscripts/verify-go-toolchain ./internal/handlers
            go test -tags kqueue,dev ./cmd -run '^Test(TrackingResponseWriter|HeadersAlreadyWritten|HeadersAlreadyWrittenWrapped|CreateEndpoints)$' -count=1
        - name: Compile supported Linux targets
          env:
            CGO_ENABLED: 0
          run: |
            GOOS=linux GOARCH=amd64 go build -tags kqueue -trimpath -o /tmp/minio-linux-amd64 .
            GOOS=linux GOARCH=arm64 go build -tags kqueue -trimpath -o /tmp/minio-linux-arm64 .
  ```

  该 job 不上传正式 artifact。

- [ ] **Step 2：将现有 workflow 锁定到生产工具链**

  对上述 11 个 workflow：

  - 顶层增加 `env: GOTOOLCHAIN: local`；
  - `1.24.x` 改为精确 `1.26.6`；
  - `check-latest: true` 改为 `false`；
  - `iam-integrations.yaml` 中不属于 matrix 的两个 job 不得继续引用不存在的
    `${{ matrix.go-version }}`，直接使用 `1.26.6`；
  - 每个 job 在构建前断言 `go env GOVERSION`，可复用同一小段 shell，不引入 composite action。

- [ ] **Step 3：运行策略检查器**

  Run:

  ```bash
  GOTOOLCHAIN=go1.26.6 go run ./buildscripts/verify-go-toolchain -root .
  ```

  Expected: PASS。

- [ ] **Step 4：校验 YAML 可解析**

  优先使用仓库/环境已有 `actionlint`；若不存在，使用 Ruby 标准 YAML parser 做语法校验，不安装
  全局包：

  ```bash
  if command -v actionlint >/dev/null 2>&1; then
    actionlint .github/workflows/*.{yml,yaml}
  else
    ruby -e 'require "yaml"; ARGV.each { |f| YAML.load_file(f) }' .github/workflows/*.{yml,yaml}
  fi
  ```

  Expected: PASS。Ruby fallback 只验证 YAML 语法；GitHub expression 语义由策略检查器和 CI 验证。

**Suggested checkpoint:** `ci: test exact Go 1.25 and Go 1.26 toolchains`

---

## Task 0.7：执行双版本、race 与跨平台验证

**Files:**

- Modify only if a verified incompatibility requires a focused fix and new regression test.

### Steps

- [ ] **Step 1：检查模块整洁性**

  Run:

  ```bash
  GOTOOLCHAIN=go1.25.13 go mod tidy -compat=1.21 -diff
  GOTOOLCHAIN=go1.26.6 go mod tidy -compat=1.21 -diff
  ```

  Expected: 两个版本均无 diff。

- [ ] **Step 2：Go 1.25.13 最低兼容验证**

  Run:

  ```bash
  GOTOOLCHAIN=go1.25.13 go version
  GOTOOLCHAIN=go1.25.13 go vet -tags kqueue,dev ./internal/handlers ./cmd
  GOTOOLCHAIN=go1.25.13 go test ./buildscripts/verify-go-toolchain ./internal/handlers
  GOTOOLCHAIN=go1.25.13 go test -tags kqueue,dev ./cmd -run '^Test(TrackingResponseWriter|HeadersAlreadyWritten|HeadersAlreadyWrittenWrapped|CreateEndpoints)$' -count=1
  GOTOOLCHAIN=go1.25.13 CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags kqueue -trimpath -o /private/tmp/minio-go125-linux-amd64 .
  GOTOOLCHAIN=go1.25.13 CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags kqueue -trimpath -o /private/tmp/minio-go125-linux-arm64 .
  ```

  Expected: 版本精确为 1.25.13，全部 PASS。

- [ ] **Step 3：Go 1.26.6 主验证**

  Run:

  ```bash
  GOTOOLCHAIN=go1.26.6 go version
  GOTOOLCHAIN=go1.26.6 go test ./buildscripts/verify-go-toolchain ./internal/handlers
  GOTOOLCHAIN=go1.26.6 go test -tags kqueue,dev ./cmd -count=1
  GOTOOLCHAIN=go1.26.6 go test -race ./internal/handlers -run '^TestForwarder' -count=1
  GOTOOLCHAIN=go1.26.6 go test -race -tags kqueue,dev ./cmd -run '^Test(CreateEndpoints|TrackingResponseWriter)$' -count=1
  GOTOOLCHAIN=go1.26.6 make verifiers
  ```

  Expected: 全部 PASS。若 `make verifiers` 需要下载 lint 工具，执行前按网络/包管理规则取得确认。

- [ ] **Step 4：全跨平台编译**

  Run:

  ```bash
  GOTOOLCHAIN=go1.26.6 bash buildscripts/cross-compile.sh
  ```

  Expected: `linux/ppc64le`、`linux/mips64`、`linux/amd64`、`linux/arm64`、`linux/s390x`、
  `darwin/arm64`、`darwin/amd64`、`freebsd/amd64`、`windows/amd64`、`linux/arm`、
  `linux/386`、`netbsd/amd64`、`linux/mips`、`openbsd/amd64`、`linux/riscv64` 全部编译成功。

- [ ] **Step 5：构建并检查 Linux 发布物**

  未提交开发态先执行：

  ```bash
  GOTOOLCHAIN=go1.26.6 CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -tags kqueue -trimpath -o /private/tmp/minio-go126-linux-amd64 .
  GOTOOLCHAIN=go1.26.6 go run ./buildscripts/verify-go-toolchain \
    -root . -binary /private/tmp/minio-go126-linux-amd64 \
    -revision "$(git rev-parse HEAD)" -goos linux -goarch amd64 -allow-modified
  ```

  在用户确认提交实现后，从 clean commit 重新构建并去掉 `-allow-modified`；只有严格检查 PASS 的
  二进制才可作为部署候选。

- [ ] **Step 6：失败分类**

  真实代码失败按 `superpowers:systematic-debugging` 处理；Docker、LDAP、OIDC、KMS 或网络服务
  缺失要记录为环境前置条件，不得修改断言让测试“通过”。

**Suggested checkpoint:** no commit unless a focused compatibility fix was required.

---

## Task 0.8：记录性能、协议与上线门禁

**Files:**

- Create: `docs/operations/go-toolchain-upgrade.md`

### Steps

- [ ] **Step 1：写可重复的开发侧 benchmark 流程**

  文档固定同一 commit、同一机器、空闲状态、`-count=10`，并分别执行：

  ```bash
  GOTOOLCHAIN=go1.25.13 go test ./internal/grid -run '^$' -bench 'Benchmark(Requests|Stream)$' -benchmem -count=10 > /private/tmp/go125-grid.txt
  GOTOOLCHAIN=go1.26.6 go test ./internal/grid -run '^$' -bench 'Benchmark(Requests|Stream)$' -benchmem -count=10 > /private/tmp/go126-grid.txt
  ```

  若环境已有 `benchstat`，使用它比较；不得在未确认时全局安装工具。开发 benchmark 只是回归
  信号，不能替代生产同规格压力测试。

- [ ] **Step 2：写生产前功能矩阵**

  文档列出必须在测试环境通过的：S3 PUT/GET/list/delete、multipart、TLS、SSE/KMS、LDAP、OIDC、
  内部 grid、Console 登录/浏览/上传，以及 Go 1.25 默认 GOMAXPROCS 的容器 CPU quota 行为。
  每项包含命令/工作流、证据链接或输出位置、负责人和结果；未执行项保持“阻断发布”，不能写成
  PASS。

- [ ] **Step 3：写生产性能对比门禁**

  同配置、同数据集、同并发、同主机分别运行当前 Go 1.24.8 生产二进制与 Go 1.26.6 候选，
  记录 throughput、p95/p99、RSS、GC CPU、pause、goroutine 和 FD。任何统计显著或业务可感知的
  回归必须先 profile；禁止预设 `nogreenteagc`。

- [ ] **Step 4：写回滚边界**

  Task 0 只产出候选和门禁。未来部署到 `10.0.1.119` 时必须先备份旧二进制、配置与 systemd
  状态，健康检查失败立即恢复旧二进制；部署仍需独立危险操作确认。

**Suggested checkpoint:** `docs: add Go toolchain upgrade runbook`

---

## Task 0.9：把 Console 实施计划接到新工具链基线

**Files:**

- Modify: `docs/superpowers/plans/2026-08-17-minio-console-iam-management.md`
- Modify: `docs/superpowers/specs/2026-08-17-go-toolchain-compatibility-design.md`

### Steps

- [ ] **Step 1：更新 Console 计划技术栈和前置关系**

  将 `Go 1.24` 改为“最低 Go 1.25.13、生产 Go 1.26.6”，并在执行约束顶部链接本计划，明确
  Task 0 未验收时 Task 1 阻塞。

- [ ] **Step 2：扩展 Console 本地模块校验**

  原计划 Task 1 的 `buildscripts/verify-console-local-module.sh` 必须额外检查：

  ```text
  console/go.mod: go 1.25.0
  console/go.mod: toolchain go1.26.6
  ```

  Console 迁入后先只改这两个 module 指令并运行 `go mod tidy -compat=1.21 -diff`；如果最终源码
  需要依赖升级才能通过 Go 1.26，单独形成失败测试和变更，不混入复制步骤。

- [ ] **Step 3：更新设计状态与验收记录**

  Task 0 全部验证通过后，将 Go 设计状态改为“已实施”；若生产性能/外部集成尚未运行，明确标为
  “部署门禁待执行”，不得把文档状态写成全面上线完成。

- [ ] **Step 4：最终计划一致性检查**

  Run:

  ```bash
  rg -n 'Go 1\.24|go1\.24|1\.24\.x|GO_VERSION="1\.16"' \
    go.mod buildscripts Makefile Dockerfile* .github/workflows docs/superpowers
  ```

  Expected: 不再出现作为当前工具链策略的旧版本；历史调研证据中的 Go 1.24 对比描述允许保留，
  但必须明确标注为旧基线。

**Suggested checkpoint:** `docs: align Console plan with Go 1.26 toolchain`

---

## Final Verification Gate

- [ ] `go run ./buildscripts/verify-go-toolchain -root .` PASS。
- [ ] Go 1.25.13 focused tests、vet、Linux amd64/arm64 static build PASS。
- [ ] Go 1.26.6 cmd tests、Forwarder/endpoint race tests、verifiers PASS。
- [ ] Go 1.26.6 全跨平台 compile PASS。
- [ ] Forwarder 不再设置 `Director`，安全与行为特征测试 PASS。
- [ ] `TestCreateEndpoints -count=20` 不依赖公共 DNS且双版本 PASS。
- [ ] 未提交态二进制元数据检查 PASS；提交后 clean build 的 `vcs.modified=false` 检查 PASS。
- [ ] 生产性能、外部 IAM/KMS/TLS 和部署验证仍作为上线门禁清晰记录，不冒充本地已验证。
- [ ] Console 计划明确继承 `go 1.25.0` / `toolchain go1.26.6`。
- [ ] `git diff --check` PASS，工作树只包含本计划授权的文件。

完成上述门禁后，才开始 `2026-08-17-minio-console-iam-management.md` 的 Task 1。
