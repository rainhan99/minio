# mds 上游来源说明

本目录是 `console/web-app` 的设计系统依赖 **mds（MinIO Design System）v1.1.5** 的内嵌副本，
由 `console/web-app/package.json` 通过 `"mds": "file:../mds"` 引用。

内嵌的原因：上游仓库 `github.com/minio/mds` 已不存在。2026-08-18 实测该仓库及其 GitHub API
对未认证访问均返回 404，使用带 `repo` scope 的已认证 token 查询同样返回 404；npm 上不存在等价包
（`@minio/mds`、`minio-mds` 均 404，npm 的 `mds` 是无关的 markdown express 路由项目）。
`console/web-app` 的 `Menu`、`MenuItem` 与全部图标组件都来自 mds，因此上游消失后前端完全无法构建。
与整体内嵌 `console/` 的决定同因同构：上游消失，转为自主维护。

## 锁定来源

| 项目 | 值 |
| --- | --- |
| 包名与版本 | `mds` `1.1.5`（`A MinIO Components Library`，AGPL-3.0-or-later） |
| 原上游仓库 | `github.com/minio/mds`（已不存在） |
| 锁定提交 | `400914d72cb3ffa27d600e0ae1f17ece2182ec22` |
| 提交标题 | `Release v1.1.5 (#1269)` |
| 提交作者与日期 | `Alex <33497058+bexsoft@users.noreply.github.com>`，2025-07-28 |
| 实际取得渠道 | `github.com/focusnetcloud/minio-mds`（原仓库删除后被 GitHub 提升为独立仓库的 fork） |
| 取得方式 | `git fetch --depth 1 origin 400914d7…` 后 `checkout FETCH_HEAD` |
| 恢复日期 | 2026-08-18 |
| 恢复规模 | 1053 个文件、约 30 MB（`dist/` 485 个 / 18 MB，`src/` 546 个 / 11 MB） |
| 完整性核对 | 全部 1053 个文件的 SHA-256 与取得的工作树逐一相同 |

### 为什么第三方 fork 可接受

不需要信任 `focusnetcloud`：

1. **提交 SHA 是内容寻址的。** SHA-1 覆盖 commit 对象与整棵 tree，`400914d7…` 对得上即意味着该提交处
   的内容与上游逐字节相同。git 在 `fetch` 时已校验全部对象哈希。该 fork 自身的 branding 改动只能存在于
   **之后**的提交，动不了这个提交。
2. **该 SHA 正是原锁定值。** 内嵌前 `console/web-app/yarn.lock` 中 mds 的 git 解析为
   `commit=400914d72cb3ffa27d600e0ae1f17ece2182ec22`，与取得的提交完全一致。
3. **提交本身来自上游。** 标题 `Release v1.1.5 (#1269)`、作者为 MinIO 的 console 工程师，不是 fork 自造的提交。
4. **依赖声明独立交叉验证。** 取得的 `package.json` 中 7 个依赖（`@types/styled-components ^5.1.34`、
   `@uiw/react-textarea-code-editor ^3.1.1`、`detect-gpu ^5.0.70`、`luxon ^3.7.1`、`react-calendar ^6.0.0`、
   `react-virtualized ^9.22.6`、`styled-components ^5.3.11`）与内嵌前 `yarn.lock` 中 mds 条目记录的 7 个
   逐一一致，这是独立于 git SHA 的第二重确认。

## 为什么用 `file:` 而不是 `portal:` / `link:`

Yarn Berry 中 `portal:` 与 `link:` 是**符号链接**，`file:` 对目录是**打包复制**（`linkType: hard`）。

实测 `portal:../mds` 不可用：

- `nodeLinker: node-modules` 下 Yarn 报 `YN0071: Cannot link mds into web-app@workspace:.
  dependency luxon@npm:3.7.1 conflicts with parent dependency luxon@npm:3.7.2`——符号链接的包无法嵌套私有
  `node_modules`；
- 且 Yarn 提示 `YN0072: The application uses portals and that's why --preserve-symlinks Node option is
  required`。webpack 默认把符号链接解析为 realpath，实测从 `console/mds` 解析 `luxon`、`styled-components`、
  `react-virtualized` 全部 `MODULE_NOT_FOUND`（`console/node_modules` 与仓库根 `node_modules` 都不存在），
  只有经符号链接路径才能解析到 `console/web-app/node_modules`。

`file:../mds` 复现了原 git 依赖的行为（同样是打包进缓存的硬链接），符号链接问题不存在，且 `yarn.lock`
重新带上 checksum：`10c0/855b08c08f5194457696ebd866466c33ecc04daa0b4f6529958b9f355cd2d91ec0b058917ae321839f3b4b89387402a3548e1fb16a6d60e35700ad98ad9d0787`。

## 本地改动记录

**本目录内容零修改**，与提交 `400914d7…` 逐字节一致（含 `dist/`、`.github/`、`.storybook/` 等 dotfiles，
`*.sh` 的可执行位也来自 git 而非人工设置）。为接入所做的改动全部在本目录之外：

1. `console/web-app/package.json`：`"mds"` 由 `https://github.com/minio/mds.git#v1.1.5` 改为 `file:../mds`。
2. `console/.gitignore`：新增 `!mds/dist/`。必须加——该文件的 `dist/` 规则会忽略 `console/mds/dist/` 全部
   485 个文件，而 `dist/` 正是被实际消费的部分（`main: dist/esm/index.js`、`types: dist/mds.d.ts`）。
   不加会导致克隆出来的仓库里该包静默损坏。例外只锚定 `mds/dist/`，`dist/` 规则在其他路径依然有效。
3. `console/web-app/yarn.lock`：mds 条目改为 `file:` 解析；并对 `luxon` 执行 `yarn dedupe`，把
   `^3.7.1` 与 `^3.7.2` 两条解析合并为单条（`^3.7.1` 本就被 `3.7.2` 满足，语义无损，同时减少一份重复打包）。

## 构建前置条件与可复现性

```bash
cd console/web-app
corepack yarn install
corepack yarn build      # = react-scripts build，等价于 web-app/Makefile 的 build-static
```

- 本机实测工具链：Node v22.20.0、Yarn 4.9.4（经 corepack 激活，`packageManager` 声明值）。
  注意 `console/web-app/.nvmrc` 与 `console/mds/.nvmrc` 声明的是 Node 18；`package.json` 未声明
  `engines`，Node 22 下 `yarn build` 实测通过。
- **确定性已验证**：同一输入连续两次 `yarn build`，112 个产物文件 SHA-256 逐字节一致。因此
  `console/web-app/build/` 的后续变化可归因于源码改动。
- **产物基线已更换**：内嵌 mds 后重新构建的 `build/` 与上游 CI 提交的原产物不同（所有 JS chunk 内容哈希
  变更，44 项增删改）。差异来自 luxon 去重与构建工具链版本差异（上游按 `.nvmrc` 用 Node 18），不是源码改动。
  仓库中的 `build/` 现为本地工具链产出的基线。
- 已知环境噪声：`pdfjs-dist` 的可选原生依赖 `canvas@3.1.0` 在本机构建失败（`YN0009`）。浏览器构建不使用它，
  `yarn build` 不受影响；依赖 canvas 的 Jest 用例可能受影响。

## 后续维护约定

- 上游已不存在，本目录是自主维护的源码，不再有"同步上游"这一路径。
- 如需修改 mds 本身，改 `src/` 后必须用 mds 自己的 `rollup.config.mjs` 重新生成 `dist/`，并在本文件记录改动；
  一旦修改，本文件"内容零修改"与 SHA-256 保真声明即失效，必须同步更新。
- 不得从来源不明的第三方 fork 合并文件；任何外部副本必须能对上提交 `400914d7…` 或有同等强度的校验依据。
