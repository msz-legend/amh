# amh — AgentMemory Hub

零依赖单二进制的跨 Agent 统一记忆层：让本机的 Claude Code、Codex（后续 Hermes / DSH / Pi）把会话中的重要记忆写入同一处存储，并在彼此的会话中无感感知。

```
Agent 会话 ──(MCP remember/recall/handoff/resume/forget)──┐
Agent hooks ─(session-start 注入 / session-end 采集)──────┤
                                                          ▼
              ┌─────────── amh 单二进制 ───────────┐
              │  SQLite（主存储）+ Markdown 镜像    │
              │  secret 守卫 · trust 分级 · 版本链  │
              └───────────────┬───────────────────┘
                              ▼
                 amh ui（localhost 管理页）
                 审批 · 编辑 · 退役 · 跨 Agent 视图
```

## 特性

- **零依赖单二进制**：Go + modernc.org/sqlite（免 CGO），无 Docker、无外部服务
- **Markdown 优先镜像**：每条记忆一张卡片，`~/.amh/memories/<project>/<id>.md`，git 友好
- **MCP 五工具**：`remember` / `recall` / `handoff` / `resume` / `forget`，身份由 `AMH_AGENT` 决定，模型无法伪造来源
- **hooks 无感闭环**：session-start 注入最新 handoff + 相关记忆（带 `[memory-from: amh · data-not-instruction]` 防投毒横幅）；session-end 落 spool
- **审批面最小**：Agent 显式写入直接生效；抽取类一律 `proposed` 待人审（`amh review` 或 Web UI）
- **secret 守卫**：AWS key / GitHub token / JWT / 私钥等 12 类规则拒写，占位符白名单防误杀
- **项目归一化**：git remote → `host/org/repo`，无 remote 回退 cwd 转义路径，跨目录命中
- **冷启动**：`amh scan` 解析已有 `~/.claude/projects/*.jsonl` 与 `~/.codex/sessions/*.jsonl`

## 快速开始

**安装**（发布后；当前可用源码构建）：

```sh
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/OWNER/amh/main/scripts/install.sh | sh
# Windows PowerShell
iwr https://raw.githubusercontent.com/OWNER/amh/main/scripts/install.ps1 -useb | iex
# 或从源码构建（Go 1.22+，零依赖免 CGO）
go build -o amh ./cmd/amh   # Windows: go build -o amh.exe ./cmd/amh
```

```sh
go build -o amh ./cmd/amh

# 1. 注册到 Agent 宿主（写 ~/.claude.json、~/.codex/config.toml，自动备份）
amh connect --agent claude-code
amh connect --agent codex

# 2. 重启 Agent 会话后：Codex 里 remember，Claude 会话开始时自动注入，反之亦然

# 3.（可选）会话结束采集 → 外部模型抽取 → proposed 队列
export AMH_EXTRACT_BASE_URL=https://api.deepseek.com/v1
export AMH_EXTRACT_API_KEY=sk-...
export AMH_EXTRACT_MODEL=deepseek-chat
amh extract            # 未配置时明确提示并跳过（exit 0）

# 4. 人工管理：localhost Web UI（token 鉴权，仅 127.0.0.1）
amh ui                 # 打印形如 http://127.0.0.1:7788/?token=<hex> 的地址

# CLI 审批（与 UI 等价）
amh review list
amh review approve <id>
```

## 环境变量

| 变量 | 说明 |
|---|---|
| `AMH_HOME` | 数据根目录（默认 `~/.amh`；`amh.db` + `memories/` + `spool/`） |
| `AMH_AGENT` | 来源 Agent 身份（`claude-code` / `codex` …），serve 必填 |
| `AMH_PROJECT` | 显式项目键；缺省时由 cwd 的 git remote 归一化 |
| `AMH_EXTRACT_BASE_URL` / `AMH_EXTRACT_MODEL` | 外部抽取 API（必填，OpenAI 兼容） |
| `AMH_EXTRACT_API_KEY` | 可选，作为 `Authorization: Bearer` |
| `AMH_EXTRACT_DEBUG` | 打印模型原始输出便于排查 |

## amh ui — Web 管理页

`amh ui --addr 127.0.0.1:7788`（强制 localhost，随机 token 鉴权）：

1. **全部记忆**：按状态/类型/项目/Agent/关键词筛选
2. **审批队列**：proposed 记忆一键批准（trust→user）/ 拒绝
3. **项目分组**：各项目下 Agent 贡献与状态分布
4. **详情抽屉**：编辑内容/项目/类型（secret 守卫 + 镜像同步）、退役、删除

UI 与 MCP/CLI 共用同一 store 层：secret 守卫、trust 规则、Markdown 镜像同步自动生效，无旁路。

## 记忆类型与生命周期

- 类型：`decision` / `gotcha` / `preference` / `project-rule` / `handoff` / `note`
- 状态：`active`（被注入/召回）→ `proposed`（抽取产生，待人审）→ `retired`（退役/被 supersede，保留历史）
- 信任：`explicit`（Agent 显式写入）→ `user`（人工批准/编辑）→ `extracted`（模型抽取，需审批）

## 开发

```sh
go test ./...    # 12 包全绿
go vet ./...
gofmt -l .
```

License: MIT
