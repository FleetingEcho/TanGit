# lazygitGo Language Server 扩展实现与来源

- 核对日期：2026-11-14。
- 本文描述**当前代码已经实现的十个扩展目标**及其上游来源，不再把早期调研中的 P1/P2/P3 建议当作产品状态。
- 版本号只是当日上游 registry/release 快照，不代表 lazygitGo 固定安装这些版本，也不代表对应运行时测试已经执行。安装器中的 `latest` 会随时间解析到不同版本。
- 代码依据：`language_server_profiles.go`、`language_server_install.go`、`language_server_expansion.go`、`language_server_release.go`、`language_server_lifecycle.go`、`language_server_settings.go`、manager/protocol 及相应测试。

## 1. 已实现的范围与准确来源

以下十个服务器均有内置 profile 和 lazygitGo managed installer：**clangd、Lua Language Server（LuaLS）、ZLS、Tombi、NeoCMakeLSP、Terraform Language Server、Intelephense、Prisma Language Server、Ruby LSP、csharp-ls**。它们都不是 `EnabledByDefault`；用户需在目录中启用，或配置可用的外部命令。

### 1.1 独立 release 的六个服务器

| ID | 官方上游与实际下载 channel | 启动 / 安装探针 | managed 平台资产选择；2026-11-14 上游快照 |
| --- | --- | --- | --- |
| `clangd` | [clangd 官方安装文档](https://clangd.llvm.org/installation)、[`clangd/clangd` releases](https://github.com/clangd/clangd/releases/latest)，经 GitHub `releases/latest` API | `clangd` / `clangd --version`；lazygitGo 不额外添加启动参数 | 只选择 Linux、macOS、Windows 的 **x86-64/amd64** standalone zip。macOS 资产名没有架构后缀，但官方 standalone 矩阵是桌面 x86-64，**没有 macOS ARM64 下载路径，也不以 x64 冒充 arm64**。快照 [`23.1.0`](https://github.com/clangd/clangd/releases/tag/23.1.0) |
| `lua` | [`LuaLS/lua-language-server`](https://github.com/LuaLS/lua-language-server)、[官方安装入口](https://luals.github.io/#install)，经 GitHub latest API | `lua-language-server` / `--version` | Linux amd64/arm64 tar.gz，macOS amd64/arm64 tar.gz，Windows amd64 zip；完整解包并保留同包资源，不只复制 binary。不支持 Windows arm64。快照 [`3.19.2`](https://github.com/LuaLS/lua-language-server/releases/tag/3.19.2) |
| `zls` | [`zigtools/zls`](https://github.com/zigtools/zls)、[官方安装与兼容说明](https://zigtools.org/zls/install/)，经 GitHub latest API | `zls` / `zls --version` | Linux/macOS amd64、arm64，Windows amd64、arm64。下载前先运行外部 `zig version`，要求 Zig 与所选 ZLS release 的 **major/minor 相同**；例如快照 [`0.17.0`](https://github.com/zigtools/zls/releases/tag/0.17.0) 要求 Zig 0.17.x。Zig 工具链不随 ZLS 安装或卸载。ZLS 是 Zig 社区项目，不应写成 Zig 官方服务器 |
| `tombi` | [`tombi-toml/tombi`](https://github.com/tombi-toml/tombi)、[官方安装页](https://tombi-toml.github.io/tombi/docs/installation) 与 [LSP 文档](https://tombi-toml.github.io/tombi/docs/language-server)，经 GitHub latest API | `tombi lsp` / `tombi --version` | 只取 `tombi-cli-*`；Linux amd64/arm64 使用 **musl** tar.gz，macOS amd64/arm64 tar.gz，Windows amd64/arm64 zip，不误选 VSIX/WASM。快照 [`v1.11.2`](https://github.com/tombi-toml/tombi/releases/tag/v1.11.2) |
| `neocmake` | [`neocmakelsp/neocmakelsp`](https://github.com/neocmakelsp/neocmakelsp) releases，经 GitHub latest API | `neocmakelsp stdio` / `neocmakelsp --version` | Linux amd64/arm64 使用 **musl** tar.gz，macOS amd64/arm64 共用 universal tar.gz，Windows amd64/arm64 zip。快照 [`v0.12.0`](https://github.com/neocmakelsp/neocmakelsp/releases/tag/v0.12.0) |
| `terraform` | [`hashicorp/terraform-ls`](https://github.com/hashicorp/terraform-ls)、[官方 release 目录](https://releases.hashicorp.com/terraform-ls/)；先查 [HashiCorp Checkpoint](https://checkpoint-api.hashicorp.com/v1/check/terraform-ls) 的 `current_version`，再构造 releases.hashicorp.com zip URL | `terraform-ls serve` / **`terraform-ls -v`** | Linux/macOS/Windows 的 amd64、arm64；Checkpoint 返回值必须是普通三段 semver。快照 [`0.39.1`](https://releases.hashicorp.com/terraform-ls/0.39.1/)。不是从 GitHub latest 猜附件 |

这六项共用精确资产名与 HTTPS 起始 URL 检查、zip/tar.gz/tar.xz 安全解包、单一路径内可执行文件查找，以及 256 MiB 下载 / 512 MiB 解压上限。当前实现**没有执行上游 SHA256、签名或 minisign 验证**；也不能把 `latest` 响应当作可复现版本锁。

### 1.2 两个 Node 包服务器

| ID | 官方来源与实际 channel | 启动与探针 | 当日快照及边界 |
| --- | --- | --- | --- |
| `intelephense` | [官方安装文档](https://github.com/bmewburn/intelephense-docs/blob/master/installation.md)、[npm `intelephense`](https://www.npmjs.com/package/intelephense)，`npm install --no-audit --no-fund --prefix <isolated>/node intelephense@latest` | 外部 `node <package>/lib/intelephense.js --stdio`；激活安装时用 Node 脚本检查实际 package manifest、版本字段与 bin 文件。profile 使用真实 LSP `initialize` 的 protocol probe，**不假设存在 `--version`** | 快照 [`1.18.5`](https://registry.npmjs.org/intelephense/1.18.5)，`SEE LICENSE IN LICENSE.txt`，是 proprietary freemium，不是全功能开源服务器。官方列出的 folding range、implementation、declaration、rename、type definition、selection range 等能力需要 licence；基础免费能力与 Premium 不能混为一谈 |
| `prisma` | [`prisma/language-tools` 官方包目录](https://github.com/prisma/language-tools/tree/main/packages/language-server)、[npm `@prisma/language-server`](https://www.npmjs.com/package/@prisma/language-server)，安装 `@prisma/language-server@latest` 到隔离 prefix | 外部 `node <package bin> --stdio`；安装探针同样是 package manifest/bin 检查，真实协议连接执行 `initialize`，**不把 `--stdio` 当版本参数，也不假设 `--version`** | 快照 [`31.13.0`](https://registry.npmjs.org/@prisma/language-server/31.13.0)，registry 明确要求 **Node.js >=20**；安装器在 npm 下载前检查 Node major，Node 19 或无法识别版本会被拒绝 |

两者的 package/bin 检查只证明安装的包结构可读。实际编辑器连接仍需完成真实 `initialize`；这也是与版本字符串探针不同的验收边界。仓库中另有 opt-in 的真实安装/协议测试，但“存在测试”不表示本文已执行。

### 1.3 依赖外部语言工具链的两个包服务器

| ID | 官方来源与实际 channel | 隔离方式、启动与探针 | 当日快照及边界 |
| --- | --- | --- | --- |
| `ruby` | [Shopify Ruby LSP 官方仓库](https://github.com/Shopify/ruby-lsp)、[官方编辑器指南](https://shopify.github.io/ruby-lsp/editors.html)、[RubyGems](https://rubygems.org/gems/ruby-lsp)；外部 `gem install ruby-lsp --no-document --install-dir <isolated>/gems --bindir <isolated>/launcher` | 先要求 PATH 上有 Ruby 与 RubyGems。wrapper 设置私有 `GEM_HOME`/`GEM_PATH` 后启动 `ruby-lsp`；探针 `ruby-lsp --version`。Ruby 本体不被托管或卸载 | 最新稳定版快照 [`0.26.11`](https://rubygems.org/gems/ruby-lsp/versions/0.26.11)，RubyGems 元数据要求 Ruby >=3.0、MIT。安装命令未固定 gem 版本，也没有为每个项目 Ruby ABI/Bundler 环境自动选择运行时 |
| `csharp` | [`razzmatazz/csharp-language-server` 官方仓库](https://github.com/razzmatazz/csharp-language-server)、[NuGet `csharp-ls`](https://www.nuget.org/packages/csharp-ls)；外部 `dotnet tool install csharp-ls --tool-path <isolated>/bin` | 要求外部 .NET SDK，启动私有 `csharp-ls`，探针 `csharp-ls --version`。SDK 与 NuGet 全局缓存不归 lazygitGo 卸载 | 快照 [`0.28.0`](https://www.nuget.org/packages/csharp-ls/0.28.0)，官方当前要求 **.NET 10 SDK+**；安装代码检查 `dotnet` 是否存在，但未固定包版本或单独验证 SDK major |

## 2. 十个目标共用的实现边界

### 2.1 检测、隔离安装、持久化、更新与卸载

1. **检测与 PATH**：未配置路径时使用 profile 的默认命令；也可在设置中填写逗号分隔的 executable candidates，按顺序检测/启动。外部 PATH 安装与 lazygitGo managed 安装都可使用同一 profile。内置服务器不会在未启用时仅因打开匹配文件就全部弹出下载提示。
2. **真正按 ID 隔离**：托管根是 `<data>/tools/lsp/<id>/`，不再是多个服务器共享的 `<data>/tools/node` 或 `<data>/tools/python`。每次安装先在 `tools/lsp` 下同层临时 staging 目录完成，再执行探针与发布；各服务器不再改写彼此的依赖树。
3. **持久化**：`settings.json` 保存 `path`、`arguments`、`probeArguments` 和可选的 `managed.root` / `managed.version`。version 是探针输出的截短显示字符串。根目录仅写 `.lazygitgo-language-server.json`，内容为 `format` 与 `id`，**不是**包含 digest、签名、ownedPaths、previousVersion 的丰富 manifest。
4. **更新**：再次执行相同 Download/Update 流程，在staging 内安装并通过探针后才改设置并原子重命名发布；候选失败会清理 staging，并保留/恢复先前 executable 设置和旧目录。成功发布后旧 backup 会删除，代码不保留可供以后回滚的 last-known-good 版本，也不承诺运行时语义失败后的自动回滚。
5. **卸载**：只有设置仍引用预期的 `<data>/tools/lsp/<id>`、profile 非 custom、目录为实际安全目录且 manifest 的 format/ID 匹配时才允许确认删除。安装或卸载进行中会锁定该服务器的配置编辑以及全局 Reset to defaults，避免异步操作把刚改过的路径误判为 lazygitGo 所有。成功卸载只删除这一 managed root，清空私有 launch/probe/managed 字段并保留 Disabled 偏好，随后恢复默认 PATH fallback。手工修改 managed 命令会撤销 lazygitGo 的删除授权，但文件不会被偷偷删除。
6. **外部失败后的 managed recovery**：编辑器语言服务器状态菜单对外部配置的 `missing` / `failed` 提供 Download 或 “Install managed”。它不是后台自动回退：由用户触发替代安装，候选探针通过前不改外部设置；安装失败会恢复原外部配置。PATH 上已有或卸载后重新出现的外部 binary 始终不属于 lazygitGo 删除范围。

### 2.2 编辑器协议能力与现有限制

十个 profile 都接入同一 additional-LSP 管线：按文件扩展名/文件名匹配，发送全量 `didOpen` / `didChange`，调用 `textDocument/hover`、`textDocument/completion`、`textDocument/definition`。大多数服务器通过 `publishDiagnostics` 显示诊断；NeoCMakeLSP 使用 `textDocument/diagnostic` 拉取完整报告，响应只有在连接、document 和 buffer version 都仍然匹配时才会应用，避免旧结果覆盖新编辑。关闭走 `shutdown → exit → 超时 kill`。上游还提供 formatting、code action、rename 等能力，不代表 lazygitGo 已把这些 UI 通路接入。

现有限制必须计入验收，不能用“有 profile”推断完整项目支持：

- 普通 additional LSP 的工作区根是当前 workspace/Git 根；除 Rust 的专门 project discovery 外，没有为这十项实现 nearest-manifest、多 root 或 solution-specific root 策略。
- NeoCMakeLSP 的 `initializationOptions` 会启用 `lint.enable`；其余扩展目标目前为 `nil`。`workspace/configuration` 只返回等长 `null` 数组，其他 server→client 请求通常成功返回 `null`。因此没有专门配置 `Lua.*`、`intelephense.*`、`csharp.solutionPathOverride`、Tombi/ZLS/Terraform 的扩展选项。
- initialize 与普通请求超时均为 8 秒，stderr 被丢弃。大型 solution、初次索引或需要构建的项目可能只能得到降级体验。Go 与 additional managed server 的非预期退出会消耗一次自动重启机会；第二次非预期退出保持 failed，由用户处理，不形成无限重试循环。主动停止、工作区切换和明确的生命周期重置会恢复这次重试预算。
- SSH workspace 会停止 additional LSP；这里只对本地平台做下载和运行，不能把本机 binary 当远端服务器。
- release 下载虽有路径穿越、非普通文件和体积保护，但尚未做 digest/签名验证；包管理器安装也未在 lazygitGo 侧锁定完整依赖树。

另需澄清现有但不是本次十项之一的来源：HTML/CSS/JSON/ESLint 使用的 [`@t1ckbase/vscode-langservers-extracted`](https://www.npmjs.com/package/@t1ckbase/vscode-langservers-extracted) 是 **T1ckbase 的第三方 repack（third-party）**，不是 Microsoft 直接发布的官方 VS Code language-server 包。其当日快照为 2.0.5、Node >=22；UI 中的 “VS Code …” 名称不构成 Microsoft 发布来源证明。

## 3. 逐项实现/验收矩阵

2026-11-14 已在 Linux amd64 上执行 `LAZYGITGO_TEST_REAL_LSP_INSTALL=1 go test ./internal/app -run 'RealProtocolLifecycle' -count=1 -v`。clangd（PATH 上已有外部 binary）、LuaLS、NeoCMakeLSP、Terraform LS、Tombi、Intelephense 和 Prisma LS 的真实协议用例通过；后六项使用测试临时目录中的 managed 下载/安装。ZLS、Ruby LSP、csharp-ls 的专用用例已接入同一测试入口，但因本机没有兼容 Zig、Ruby、.NET 外部工具链而明确跳过，**不能记为真实运行时通过**。这些结果不代表其他 OS/CPU、CI 或所有上游版本都已验收。

| 候选 | 检测、隔离安装与探针 | 持久化 / 更新 / 卸载 | 外部失败恢复与 PATH fallback | `didOpen` / `didChange` 与语义能力；诚实验收状态 |
| --- | --- | --- | --- | --- |
| clangd | PATH/自定义 candidates 或 `lsp/clangd` 精确 zip；`--version` | 共用第 2.1 节完整流程 | 同样支持 Install managed，卸载后回到 PATH `clangd` | 四项编辑器语义通路均已接。仓库有本机 clangd 存在才执行的 `TestClangdRealProtocolLifecycle`，覆盖 open/change、hover、completion、definition、diagnostics；没有 compilation database 的大型项目验收记录 |
| LuaLS | PATH 或完整 LuaLS release 隔离目录；`--version` | 同上；卸载保留 `.luarc.json*` 和用户 library | Install managed；回到 PATH `lua-language-server` | 2026-11-14 managed 真实用例通过 open/change、hover、completion、definition、diagnostics；`Lua.*` workspace configuration 尚未提供 |
| ZLS | PATH 或匹配 Zig major/minor 后的隔离 release；`--version` | 同上；只拥有 ZLS root，绝不拥有 Zig | Install managed；回到 PATH `zls`，不匹配时仍留给工具链处理 | 专用真实用例存在，但本机没有兼容 Zig，2026-11-14 明确跳过；固定 Zig+ZLS 兼容对、build workspace 与不同 patch 组合仍待验收 |
| Tombi | PATH 或精确 `tombi-cli` archive；`--version` | 同上；不拥有项目 TOML/schema cache | Install managed；回到 PATH `tombi` | 2026-11-14 managed 真实用例通过五项基本语义；schema catalog 网络行为、本地 schema、离线模式未形成运行矩阵 |
| NeoCMakeLSP | PATH 或精确 release archive；`--version` | 同上；不拥有 CMake、build 目录或项目配置 | Install managed；回到 PATH `neocmakelsp` | 2026-11-14 managed 真实用例通过初始化进度、open/change、项目本地 symbol hover/completion、显式 `.cmake` include definition 和 pull diagnostics；测试不依赖外部 CMake catalog。CMake cache、watched files、外部分析依赖及 macOS/Windows 仍待验证 |
| Terraform LS | PATH 或 Checkpoint → HashiCorp release zip；**`-v`** | 同上；绝不拥有 `.terraform`、state、provider 或 Terraform CLI | Install managed；回到 PATH `terraform-ls` | 2026-11-14 managed 真实用例通过五项基本语义；provider schema、backend 网络、root module 与离线行为未作完成承诺 |
| Intelephense | PATH 或隔离 npm prefix；**package manifest/bin 检查 + protocol initialize，非 `--version`** | 同上；Node 与默认许可位置不属于 managed root | Install managed；回到 PATH `intelephense` | `TestIntelephenseRealProtocolLifecycle` 于 2026-11-14 managed 安装后通过五项基本语义。未配置 licenceKey/storagePath，Premium 能力不作已配置承诺 |
| Prisma LS | PATH 或 Node>=20 门禁后的隔离 npm prefix；**package manifest/bin 检查 + protocol initialize，非 `--version`** | 同上；Node 外部 | Install managed；回到 PATH `prisma-language-server` | `TestPrismaRealProtocolLifecycle` 于 2026-11-14 managed 安装后通过五项基本语义。Prisma formatting/code actions/rename 等上游能力没有因此获得 lazygitGo UI 通路 |
| Ruby LSP | PATH 或私有 gem home + launcher wrapper；`--version` | 同上；Ruby、RubyGems、项目 bundle/cache 外部 | Install managed；回到 PATH `ruby-lsp` | 专用真实用例覆盖 indexed constant hover/completion/definition 和 change diagnostics，但本机没有 Ruby，2026-11-14 明确跳过；不同 Ruby ABI、rbenv/mise/asdf 与 `bundle exec` per-project 环境尚未自动处理或验收 |
| csharp-ls | PATH 或私有 `dotnet --tool-path`；`--version` | 同上；.NET SDK、NuGet 全局缓存、solution/project 文件外部 | Install managed；回到 PATH `csharp-ls` | 专用真实用例包含 SDK project，但本机没有 .NET，2026-11-14 明确跳过；`.cshtml` 未映射，未实现 `csharp:/` metadata handler 或 solution options，大 solution 的首次加载仍需专项验收 |

安装选择、Checkpoint、Ruby/C# 前置条件与 Node 20 的单元测试位于 `language_server_expansion_install_test.go`；它们使用 fake HTTP/工具调用，不能替代真实服务器验收。生命周期 mock 覆盖候选失败不改外部设置、managed 持久化、确认卸载、manifest 拒绝、PATH fallback、失效 managed 文件、发布/卸载失败、重复动作、卸载期间的配置锁定、protocol-only probe、一次性崩溃恢复，以及 NeoCMake pull diagnostic 的 stale response 拒绝。Python 另有使用真实本地 venv、无网络 fake package 的 staging 发布测试，验证重命名后 interpreter/entry-point 启动仍可执行，不依赖 pip console script 中的临时 shebang。真实 release 服务器用例位于 `language_server_release_expansion_integration_test.go`，Intelephense/Prisma 位于 `language_server_node_expansion_integration_test.go`，Ruby/C# 位于 `language_server_toolchain_expansion_integration_test.go`。真实六平台 release、包更新、签名破坏、运行时更新恢复及外部工具链组合仍是后续发布验收工作。

## 4. 仍然 deferred 的候选

“Deferred”表示没有对应内置 managed/profile 实现，不再误写为当前 detect-only 已交付。用户可以用 custom definition 接外部命令，但这不等于 lazygitGo 已验收该项目工具链。

| 候选 | 保持 deferred 的官方原因与来源 |
| --- | --- |
| Java / Eclipse JDT.LS | [官方 README](https://github.com/eclipse-jdtls/eclipse.jdt.ls) 当前要求 Java 25，wrapper 还需 Python 3.9；每 workspace 独立 `-data`、平台 configuration、Maven/Gradle 导入、较长启动和 server JVM 与项目 JVM 的区分都尚未由 lazygitGo 实现。JDK 不应随服务器托管/卸载 |
| Kotlin LSP | [`Kotlin/kotlin-lsp`](https://github.com/Kotlin/kotlin-lsp) 当前是 Alpha、read-only mirror，发行内容含 JetBrains Air/Fleet 部分闭源组件，运行要求 JDK 25；Gradle/Maven 导入、长超时、notices 与 macOS `.sit` 包处理尚未具备。不要把仓库 Apache-2.0 简化成全部发行组件的来源结论 |
| Swift / SourceKit-LSP | [`swiftlang/sourcekit-lsp`](https://github.com/swiftlang/sourcekit-lsp) 随 [Swift toolchain](https://swift.org/install/) 或 Xcode 提供，不应下载与项目 Swift 不匹配的独立 binary；跨模块结果依赖项目 build，Swift/Xcode/SDK 的更新与删除归外部工具链 |
| Dart analysis server | server 在 Dart SDK 内，官方命令与 SDK 来源见 [`dartdev` 的 `language_server.dart`](https://github.com/dart-lang/sdk/blob/main/pkg/dartdev/lib/src/commands/language_server.dart) 和 [Dart SDK 安装页](https://dart.dev/get-dart)。Flutter 项目必须选匹配 SDK，并先完成 `pubspec.yaml` / package config 所代表的依赖解析；lazygitGo 不另建 server 下载矩阵 |
| Haskell Language Server | [官方仓库](https://github.com/haskell/haskell-language-server) 与[安装文档](https://haskell-language-server.readthedocs.io/en/latest/installation.html) 以 ghcup、wrapper 和项目 GHC 对应 binary 为核心；Cabal/Stack cradle、GHC ABI 组合、动态库与首次加载不能只靠一个全局 server binary 解决。ghcup/GHC/cabal/Stack 均不归 lazygitGo 卸载 |

当前实现的原则仍然是：**lazygitGo 可以拥有一个隔离的 server 副本，但不因此拥有语言 SDK、项目依赖、构建缓存、许可密钥或用户 PATH 上的安装。**