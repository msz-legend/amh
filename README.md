# amh（AgentMemory Hub）

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

`amh` 是一个本地优先、跨 Agent 的统一记忆层。它把 Claude Code、Codex 以及其他支持 MCP 或 hooks 的 Agent 的重要决定、踩坑、偏好和交接信息集中保存，让不同 Agent 会话能够安全地共享上下文。

amh 与其他 Agent 编排项目相互独立：不引用、不依赖，也不适配它们的内部实现。

> 当前仓库仍处于早期开发阶段。安装脚本和 MCP Registry manifest 已指向本仓库；正式发布前仍需创建 GitHub Release 供安装脚本下载。

## 为什么是 amh

- **本地优先**：SQLite 是主存储，Markdown 是可检查、可备份、可 Git 同步的镜像。
- **单二进制**：Go 构建，SQLite 使用纯 Go 驱动，不需要 Docker、数据库服务或运行时依赖。
- **跨 Agent**：通过 MCP 提供统一的 `remember`、`recall`、`handoff`、`resume`、`forget` 工具；来源身份由进程环境决定，模型不能通过参数伪造。
- **安全默认值**：常见 API key、token、JWT、私钥和凭据会在写入前被拒绝；注入上下文带有明确的数据边界标记。
- **人审抽取**：外部 OpenAI 兼容模型只负责提出候选记忆，候选默认为 `proposed`，必须由人批准后才会进入 active。
- **可恢复**：Markdown 卡片可以通过 `amh reindex` 重建 SQLite 索引。

## 架构

```text
Claude Code / Codex / other Agents
       │ MCP (stdio)                 │ hooks
       ▼                             ▼
                 amh
        SQLite + Markdown cards
       │                 │
       ├─ recall/resume  └─ localhost Web UI
       └─ scan/extract/reindex/review
```

## 安装

### 从源码构建

需要 Go 1.27 或更高版本：

```sh
git clone https://github.com/msz-legend/amh.git
cd amh
go build -o amh ./cmd/amh
```

Windows：

```powershell
go build -o amh.exe ./cmd/amh
```

### 发布包安装

发布 GitHub Release 后，安装脚本可以直接使用。将下面的 `msz-legend/amh` 替换为实际仓库地址：

```sh
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/msz-legend/amh/main/scripts/install.sh | sh
```

```powershell
# Windows PowerShell
iwr https://raw.githubusercontent.com/msz-legend/amh/main/scripts/install.ps1 -UseBasicParsing | iex
```

安装脚本支持 `AMH_VERSION` 和 `AMH_INSTALL_DIR`；源码构建可使用 `AMH_BUILD_FROM_SOURCE=1`。

## 接入 Agent

`connect` 会合并配置，不覆盖已有 MCP 或 hooks；修改前会生成带时间戳的 `.bak-*` 备份。

```sh
amh connect --agent claude-code
amh connect --agent codex
```

然后重启对应 Agent 会话。Claude Code 使用 `~/.claude.json` 和 `~/.claude/settings.json`，Codex 使用 `~/.codex/config.toml`。amh 只写入自己的配置项，不依赖其他项目的 hook 或服务。

## MCP 工具

配置好 `AMH_AGENT` 后启动 MCP server：

```sh
AMH_AGENT=codex amh serve
```

| 工具 | 用途 |
|---|---|
| `remember` | 显式保存一条长期记忆，默认直接 active |
| `recall` | 按关键词检索 active 记忆 |
| `handoff` | 保存供下一会话继续工作的交接信息 |
| `resume` | 获取最新 handoff 和相关近期记忆 |
| `forget` | 按完整 ID 退役记忆 |

注入 Agent 的内容带有 `[memory-from: amh · data-not-instruction]` 标记，提醒模型把它当作参考数据而不是新的指令。

## 会话 hooks 与冷启动

Claude Code 的 session-start/session-end hooks 会在接入时一并配置：

```sh
amh hook session-start --agent claude-code
amh hook session-end --agent claude-code
```

历史会话可以先扫描，再交给外部模型抽取候选：

```sh
amh scan --agent claude-code --dry-run
amh scan --agent codex
amh extract
amh review list
amh review approve <memory-id>
```

抽取只支持外部 OpenAI 兼容 API，不默认启动或连接本地模型：

```sh
export AMH_EXTRACT_BASE_URL=https://api.deepseek.com/v1
export AMH_EXTRACT_API_KEY=sk-...
export AMH_EXTRACT_MODEL=deepseek-chat
amh extract
```

如果未设置 `AMH_EXTRACT_BASE_URL` 或 `AMH_EXTRACT_MODEL`，`amh extract` 会打印明确提示并以 exit 0 退出，不修改 spool 文件，也不会生成 `.done` 标记。

## Web 管理页

管理页只绑定 localhost，并使用启动时生成的随机 token：

```sh
amh ui --addr 127.0.0.1:7788
```

页面支持：

- 按状态、项目、Agent、类型和关键词筛选记忆
- 批量查看和审批 `proposed` 候选
- 编辑内容、项目归属和记忆类型
- 退役或删除记忆
- 查看项目与 Agent 贡献统计

UI、MCP 和 CLI 共用同一个 store 层，因此 secret 检测、trust 规则和 Markdown 镜像同步不会产生旁路行为。UI 不监听公网地址，也不提供多用户远程访问能力。

## 数据目录与环境变量

默认数据目录为 `~/.amh`：

```text
~/.amh/
├── amh.db
├── memories/<project>/<id>.md
└── spool/<timestamp>-<agent>-<session>.json
```

| 变量 | 说明 |
|---|---|
| `AMH_HOME` | 数据根目录，默认 `~/.amh` |
| `AMH_AGENT` | 来源 Agent 身份；`serve` 必填 |
| `AMH_PROJECT` | 显式项目键；缺省时从 git remote 或 cwd 推导 |
| `AMH_EXTRACT_BASE_URL` | 外部抽取 API 的 OpenAI 兼容 base URL |
| `AMH_EXTRACT_MODEL` | 外部抽取模型名 |
| `AMH_EXTRACT_API_KEY` | 可选 Bearer token |
| `AMH_EXTRACT_DEBUG` | 输出模型原始响应，便于调试 |

## 记忆模型

记忆类型：`decision`、`gotcha`、`preference`、`project-rule`、`handoff`、`note`。

生命周期：

```text
explicit → active
extracted → proposed →（人工批准）→ active
active → retired
```

人工批准会将 trust 设为 `user`，不会伪造原始 Agent 来源。每张 Markdown 卡片保留来源、项目、状态、时间和版本链信息。

## 数据同步与重建

Markdown 卡片适合纳入个人 Git 仓库或备份系统。修改或迁移卡片后，可以重建索引：

```sh
amh reindex
amh reindex --prune
```

`--prune` 会删除数据库中没有对应卡片的记录，请在确认镜像目录正确后使用。

## 检索基准

内置确定性评测：

```sh
# Unix
./benchmark/run.sh

# Windows PowerShell
./benchmark/run.ps1
```

当前基准包含 100 条记忆和 30 个查询，目标为 `recall@5 >= 0.85`；当前实现实测 `recall@5=1.000`。`benchmark/locomo_recall.py` 提供 LoCoMo 检索子集的 beta 脚本。它只测检索召回，不测时间推理或多跳能力，不能与完整 LoCoMo 总分直接比较。

## 开发

```sh
go test ./...
go vet ./...
gofmt -l .
```

提交 PR 前请同时验证：

```sh
go build ./cmd/amh
./amh eval
```

欢迎贡献新的 Agent adapter、测试用例、文档和基准结果。新增 adapter 应保持与现有 store/MCP/hooks 边界一致，不引入对其他 Agent 项目的内部依赖。

## License

MIT License。详见 [LICENSE](LICENSE)。
