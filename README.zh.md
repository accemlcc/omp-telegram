# omp-telegram

[English](README.md) | 中文

独立 Go 守护进程, 把 Telegram 已有 topic 连接到 `omp --mode rpc`. 每个 topic 使用独立 omp 进程和会话, 同 topic 的普通文本串行执行, 不同 topic 可并行. 设计与验收范围见 [PLAN.md](PLAN.md), 维护约束见 [AGENTS.md](AGENTS.md).

## 运行要求

- Linux 或 WSL2 Linux 环境. 使用 Unix 进程组和文件锁, 不支持直接运行 Windows 二进制.
- 构建需要 Go 1.26 或更高版本, [just](https://github.com/casey/just), 以及可访问的 Go 模块源. SQLite 使用纯 Go 驱动.
- 已安装可运行的 omp 及其运行时依赖, 并为运行服务的用户配置好模型和认证. RPC 必须支持已知的 ready/分帧协议和协商 v2; 不兼容的版本会拒绝启动, 不回退到终端模拟.
- 可访问 Telegram Bot API 和所选模型服务. 一个 bot 只运行一个 polling 实例, 不能同时保留 webhook 或其他 getUpdates 消费者.

## Telegram 准备

1. 在官方 `@BotFather` 创建自己的 bot, 私下保存 token.
2. 推荐在 supergroup 中启用 Topics, 由管理员手动创建 topic, 把 bot 加入群并允许发送消息. 不自动创建或重新打开 topic, 不需要为了创建 topic 授予管理权限.
3. bot 必须能收到普通文本: 在 BotFather 的 `/setprivacy` 中为它关闭 privacy mode, 按 Telegram 提示重新加入群; 或按实际需要赋予管理员身份. 优先使用最少权限. 仅能接收 slash command 不足以进行普通文字对话.
4. 将你自己的数字 user ID 和目标数字 chat ID 填入白名单; 群 chat ID 通常是负数. 可在启动 daemon 前用自己控制的 Bot API 客户端查看 getUpdates 中的 `message.from.id` 和 `message.chat.id`, 不要把 token 交给第三方 ID 查询网站. 管理员以个人身份发送, 不使用匿名管理员或频道身份.
5. 在 topic 内发送 `/new test`, 直接在 `<workspace_root>/test` 新开 omp 会话; 或用 `/new /tmp/test` 直接在 `/tmp/test` 工作. 不存在的目录自动创建, 已有文件不复制也不清空. 已绑定 topic 可省略参数复用原目录. `/resume <omp-session-id>` 恢复已有的原生会话及其工作目录. 私聊需要 Telegram 的 threaded mode.

## 构建, 安装与运行

```sh
just
chmod 600 config.toml
```

`just` 和 `just build` 编译 `./omp-telegram`. `just install` 编译并仅安装二进制到 `~/tool/omp-telegram/omp-telegram`; 可用 `just install /your/bin/directory` 指定其他目录. 不复制配置和数据. `just test` 依赖 `install`, 完成编译和安装后执行 `supervisord ctl restart omp-telegram`, 使用你现有的 go-supervisor 配置. 该服务应运行默认安装目录中的二进制. 服务参数和环境变量来自 supervisor, 不来自调用命令的 shell. `just check` 执行单元测试, race 检查和 vet, 不启动 Telegram polling. 部署方式仍由用户自行管理.

使用 `--config <path>` 或 `-c <path>` 指定配置文件, 用 `--check` 检查配置而不启动服务. 例如: `./omp-telegram -c config.local.toml --check`.

不传 `--config` 时, 桥接优先读取解析符号链接后的真实可执行文件目录中的 `config.toml`. 文件不存在时, 使用编译时内嵌的仓库 `config.toml`, 不生成配置文件. 已有文件无法读取或格式错误时仍报错; 显式 `--config` 路径不存在也报错. 内嵌配置要求进程环境提供 `OMP_TELEGRAM_BOT_TOKEN`, `OMP_TELEGRAM_ALLOWED_USERS` 和 `OMP_TELEGRAM_ALLOWED_CHATS`. 安装到其他位置仍会改变默认数据和工作目录位置, 不迁移已有数据. 前台运行时, 设置所需环境变量后执行 `./omp-telegram`; `./omp-telegram --check` 检查配置, `./omp-telegram --config config.local.toml` 使用私有配置. 显式相对配置路径相对于调用目录. 不要同时用前台和 supervisor 运行使用同一 bot 或数据库的实例. 前台实例用 Ctrl-C 停止. 不向受版本控制的模板写入凭据, 无需静态项目列表.

| 字段 | 含义 |
| --- | --- |
| `token` | Bot token; 推荐使用 `"${OMP_TELEGRAM_BOT_TOKEN}"`, 不直接写入秘密. 省略时默认使用该引用 |
| `allowed_users`, `allowed_chats` | 数字 ID 数组, 两个白名单必须同时匹配; 无自动配对 |
| `workspace_root` | 动态工作目录的根路径; 默认读取 `OMP_TELEGRAM_WORKSPACE_ROOT`, 变量未设置或为空时使用真实可执行文件目录下的 `workspace/` |
| `omp` | 默认 `"omp"`, 通过 `PATH` 查找; 可执行文件名或路径, 不是含参数的 shell 命令 |
| `omp_args` | 可选的带引号启动参数字符串, 默认 `${OMP_TELEGRAM_ARGS}`; 变量未设置或为空时不加额外参数 |
| `data_dir` | 默认 `.`: `omp-telegram.db` 和 `daemon.lock` 直接放在真实可执行文件目录下 |
| `max_workers` | 同时运行的 omp 进程上限 |
| `queue_capacity` | 每 topic 等待执行的普通文本队列容量 |

相对工作目录根路径和数据目录相对于真实可执行文件目录, 不是启动工作目录或配置文件所在目录. 绝对路径保持不变. 同一数据目录不能运行两个 daemon; 不要给不同 bot 共用数据目录.

默认桥接配置, 状态和工作目录都在可执行文件旁: `config.toml`, `omp-telegram.db`, `daemon.lock`, `workspace/`. 这是桥接配置, 不是 omp 配置. 除非你通过 `omp_args` 显式指定启动选项, 否则 omp 的模型, 认证, 审批, 扩展和历史均保留自身默认行为. 本程序不自动传 `--session-dir`, 不搬动历史目录, 不维护第二份模型对话, 只记录 omp 返回的 session 路径供恢复使用. SQLite 运行时还可能生成 `omp-telegram.db-wal` 和 `omp-telegram.db-shm`, 不要删除它们, 也不要在写入期间只复制主数据库. 数据库文件保持私有权限, 不修改已有项目目录的权限.

字符串值支持 `$VAR` 和 `${VAR}` 环境变量引用, `$$` 表示字面 `$`. TOML 解析后只展开一次, 变量内容不能注入 TOML 字段. 缺失变量通常会报错. `workspace_root` 恰好引用 `OMP_TELEGRAM_WORKSPACE_ROOT` 时, 变量未设置或为空则回退到可执行文件目录下的 `workspace/`; 省略字段使用该引用, 显式空字段使用同样的回退路径. `omp_args` 中的 `OMP_TELEGRAM_ARGS` 也是可选变量, 见下文. 显式非空配置优先. 不支持 `${VAR:-fallback}` 等 shell 默认值语法, 命令替换或自动加载 `.env`.

ID 和数量字段支持 TOML 整数或带引号的十进制字符串. 白名单的字符串元素还支持逗号分隔的多个 ID, 包括环境变量展开后的值; 忽略每项两侧的空白, 但空项或非法 ID 会导致加载失败. 数量字段仍要求单个整数. 引用必须加引号, 不要直接写未加引号的 `${VAR}`. 白名单仍需保留下面示例中的 TOML 数组括号:

```toml
token = "${OMP_TELEGRAM_BOT_TOKEN}"
allowed_users = ["${OMP_TELEGRAM_ALLOWED_USERS}"]
allowed_chats = ["${OMP_TELEGRAM_ALLOWED_CHATS}"]
omp = "omp"
omp_args = "${OMP_TELEGRAM_ARGS}"
data_dir = "."
workspace_root = "${OMP_TELEGRAM_WORKSPACE_ROOT}"
max_workers = "${OMP_TELEGRAM_MAX_WORKERS}"
queue_capacity = 16
```

启动前 export 所有引用的变量, 或通过自行选择的进程管理器传入. 仓库示例需要 `OMP_TELEGRAM_BOT_TOKEN`, `OMP_TELEGRAM_ALLOWED_USERS` 和 `OMP_TELEGRAM_ALLOWED_CHATS`. 上述示例还需设置 `OMP_TELEGRAM_MAX_WORKERS`. 多用户/多 chat 可以设置 `OMP_TELEGRAM_ALLOWED_USERS=123456789,987654321` 和 `OMP_TELEGRAM_ALLOWED_CHATS=123456789,987654321,-1001234567890`, 也可以直接写 TOML 数字数组. 私聊 chat ID 与对应用户的 user ID 相同, 群 chat ID 为负数. 鉴权要求同时命中两个全局白名单, 不是按用户分别分配 chat.

`/new` 新建的是 omp 进程和对话, 不是额外的文件系统身份. `/new test` 直接使用 `<workspace_root>/test`, `/new /tmp/test` 直接使用 `/tmp/test`. 绝对路径和 `~`/`~/...` 可以尚不存在: 桥接解析已有父目录的符号链接, 再创建指定目录. 已有目录和文件原样保留. 普通名称必须是单个目录名, 不能是 `.`, `..`, 带层级的相对路径或包含控制字符的字符串; 不接受命名目录符号链接或悬空链接. 不再添加路径编码分组或随机 ID 目录.

未绑定 topic 的首次 `/new` 必须提供名称/路径. 已绑定 topic 的无参 `/new` 在保存的原目录新开一个原生会话, 即使 `workspace_root` 已改变也一样. 替换运行中的实例需要确认. 不同 topic 可以显式选择同一目录: 对话彼此独立, 但文件共享, 并发修改可能冲突.

使用 `/resume` 选择该 topic 当前工作目录中的原生 omp 会话. 列表由短时运行的 `omp acp` 通过 `session/list` 提供, 桥接不扫描 session 文件, 不以自身数据库历史代替列表. 每页显示 8 个会话, 包含标题, ID, 更新时间及 Previous/Next/Cancel 按钮. 选择后替换空闲实例; 正在执行任务, 压缩或存在排队消息时禁止切换. 旧回调, 过期回调和其他用户的回调不能切换会话. 尚未绑定目录的 topic 需先使用 `/new <name or path>` 或显式 ID 快捷方式.

`/resume <omp-session-id>` 保留为快捷方式, 未绑定 topic 也可使用; 已有实例需先关闭. 支持至少 8 个字符的十六进制 ID 前缀, 建议使用完整 ID. 由 omp 恢复会话及原目录. 桥接读取 RPC 身份和本地 `/session info` 元数据, 保存原生 session 文件路径, 并在就绪消息及 `/status` 显示原生 ID. 同一原生 session 不能在多个桥接 topic 同时运行. 原目录缺失时报错而非重建. 尚未被 omp 持久化历史的新空会话可能暂时不能恢复.

### 显式指定 omp 启动参数

如果需要 Telegram 专用的 omp 配置 overlay, 在桥接程序的 `config.toml` 中保留:

```toml
omp_args = "${OMP_TELEGRAM_ARGS}"
```

在 shell 中设置环境变量, 含空格的路径要加引号:

```sh
export OMP_TELEGRAM_ARGS="--config \"$HOME/.config/omp/telegram.yml\""
```

也可以改为直接配置字符串:

```toml
omp_args = '--config "${HOME}/.config/omp/telegram.yml"'
```

这个文件由你按 omp 的配置格式准备. 桥接程序只把路径传给 omp, 不创建或修改 omp 配置文件. 也可以显式传入 `--profile`, `--model` 等选项. 每次启动 worker 都使用这些参数, 包括 `/resume`; 修改配置或环境变量后需重启 daemon 才能加载.

字符串按 shell 风格的引号和反斜杠转义规则分词, 再直接作为 argv 传入, 不执行 shell, 命令替换, 通配符匹配或 `~` 展开. 从 `OMP_TELEGRAM_ARGS` 读取的内容不会递归展开环境变量: 像上述示例一样在 shell 中展开 `$HOME`, 或填写实际绝对路径. 与桥接数据目录不同, omp 参数中的相对路径由 omp 在 worker 工作目录中解释. 配置 overlay 请使用绝对路径.

省略 `omp_args` 时默认读取可选变量 `OMP_TELEGRAM_ARGS`, 未设置或为空就不加额外参数. 显式 `omp_args = ""` 会禁用额外参数, 即使环境变量有值. 引号不匹配时配置加载失败, 不把参数值写入错误日志.

RPC 模式, 工作目录和会话生命周期仍由桥接管理. `omp_args` 不允许 `--mode`, `--cwd`, `--resume`/`--session`/`-r`, `--continue`/`-c`, `--print`/`-p`, `--no-session` 或 `--` 分隔符. 其他显式选项交给 omp 解释, 校验和权限影响也由 omp 决定.

在 Bash 中交互式读取 token, 避免把值直接写入命令历史:

```sh
export OMP_TELEGRAM_ALLOWED_USERS=123456789
export OMP_TELEGRAM_ALLOWED_CHATS="$OMP_TELEGRAM_ALLOWED_USERS"
read -r -s -p 'Telegram bot token: ' OMP_TELEGRAM_BOT_TOKEN; printf '\n'
export OMP_TELEGRAM_BOT_TOKEN
just build
./omp-telegram --check
./omp-telegram
```

`./omp-telegram --check` 检查配置, 不启动 Telegram polling: 解析 TOML, 展开变量, 校验启动参数和 omp 可执行文件, 创建数据目录和工作目录根路径. 除可选工作目录和 omp 参数变量外, 引用的变量必须已设置. 它不验证模型认证, 不解析 omp 自己的配置文件. 使用 supervisor 管理实例时, 在 supervisor 中设置环境变量, 用 `just test` 重新编译并重启.

## Topic 命令

Telegram 界面目前仅使用英语: 斜杠命令描述, `/help`, 状态, 确认按钮, 进度标签和桥接错误均为英文, 不随客户端语言切换. daemon 启动时注册英文命令列表, 之前注册的中文列表也会替换为英文. 在 topic 输入 `/` 查看提示, 如有旧缓存请重新进入聊天. 注册失败会明确报错并停止启动. 用户消息, 模型回复及扩展提供的对话内容不自动翻译. README.md 与 README.zh.md 仍保留中英文文档. 能看到菜单不代表拥有执行权限.

| 命令 | 行为 |
| --- | --- |
| `/new [名称或路径]` | 直接在指定目录新开 omp 会话, 目录缺失时创建; 后续省略参数复用原目录, 已有文件不动 |
| `/stop` | 请求中止当前任务, 清空等待文本, 保留会话 |
| `/close` | 关闭实例并清空等待文本, 保留历史 |
| `/resume [omp-session-id]` | 无参时通过分页按钮选择当前目录的原生会话; 指定 ID 时直接恢复, 需先关闭已有实例 |
| `/status` | 查看工作目录, 原生 session ID, 模型, 忙闲和队列状态; 未运行时提示全局不确定记录数 |
| `/model` | 查看当前状态和模型 |
| `/model provider/model` | 在空闲时切换模型 |
| `/compact` | 在空闲时经按钮确认压缩上下文 |
| `/help`, `/start` | 显示帮助 |

普通文本只发送给当前已启动的实例; 不因文本自动启动进程. 可使用 `/command@botname`, 未知 slash command 不透传给 omp. 文本输出使用节流预览和分段最终消息, 不转发原始 RPC 状态或完整工具输出.

RPC 的 `confirm` 和有限数量的 `select` 交互使用按钮, 超时取消. 只有 omp 通过 RPC 暴露时, 它们才能承载权限请求; 这不是所有终端权限弹窗的镜像. `input`/`editor` 输入界面暂不支持, 会明确取消. 桥接不会启用自动批准, 不修改 omp 配置文件, 不覆盖模型/认证/审批设置. 图片文件传输通过下述桥接专用 RPC host tool `telegram_send` 提供; 其他配置变更仍必须来自显式 `omp_args` 或 `/model` 等命令.

## 图片和文件

在已运行 omp 的 topic 内, 直接使用 Telegram 的图片或附件按钮发送, 不需要上传命令. Caption 作为 prompt; 没有 caption 时请 omp 查看附件. Caption 是普通提示文字, 不作为桥接斜杠命令执行. 相册中的各条消息分别处理, 同一 topic 内顺序提交.

原文件保存在当前工作目录的 `.telegram/incoming/` 下, 文件名经过清理, 下载路径限制在该工作目录内. 支持的 JPEG/PNG/GIF/WebP 图片会通过 RPC prompt 传入. 内联预算为 512 KiB: 大图生成有界 JPEG 预览, 原件仍可通过本地路径访问. 超过 1600 万解码像素的图片, 不支持的格式及普通文档通过本地路径提供, 并明确说明, 不伪装成已内联的图片. 理解图片需要模型具备相应能力; 读取本地文档取决于 omp 配置的工具.

已提交给 omp 的原件会保留供该会话使用. 准备失败或取消的附件会清理; 中止任务不会删除已经提交给 omp 的文件.

回传时直接说“把报告发给我”或“把图片发回来”. omp 可调用 `telegram_send`, 参数为 `path`, 可选 `kind` (默认 `document`, 或 `photo`) 和可选 `caption`, 不需要 `/sendfile` 命令. 工具只接受当前工作目录内的普通文件, 仅在当前 Telegram 请求执行期间可用, 不能指定其他 chat/topic. 文件先复制为 `<data_dir>/attachments/outbox/` 下的私有快照, 再持久化入队. 工具成功只表示入队, 不代表已送达; 确认上传后删除快照, 不确定的交付保留快照且不自动重发.

使用 Telegram 官方托管 Bot API 时, 下载上限为 20 MB, 文档发送上限 50 MB, 图片发送上限 10 MB. `photo` 只接受尺寸符合 Telegram 限制的 JPEG/PNG; 原图或其他格式可按 `document` 发送. Caption 上限为 1024 个 UTF-16 code units. 下载和准备异步且限制并发, `/stop` 不会排在文件传输之后; 单次传输请求超时为五分钟. 文件下载前执行鉴权. 数据库只保存附件元数据和交付状态, 不保存二进制内容.

不支持语音消息, 转写或自动创建 topic. 桥接不会扫描目录并自动上传所有生成文件.

## 恢复与安全边界

- daemon 启动时自动恢复上次退出时仍运行的 topic 实例, 使用保存的准确原生 session 文件路径及工作目录. `/close` 关闭自动恢复资格; `/stop` 保留实例和恢复资格. 仅恢复当前 bot 及当前白名单内的 chat, 并遵守 `max_workers`. 会话或目录缺失, 容量不足时保留原身份和恢复意图; 排除问题后可用 `/resume`, 或在下次重启时重试. 不静默创建替代会话.
- SQLite 的 `PRAGMA user_version` 记录数据库 schema 版本, 当前为 1. 新空库在同一事务创建当前结构并写入版本号. 开发阶段不迁移旧结构: 已有无版本库及不支持的版本均拒绝打开, 不改变其表结构和记录. 应先备份不兼容数据库并使用新的数据目录, 不要仅修改版本号冒充兼容. 正式发布后的结构变更需要显式逐版本事务迁移.
- 已提交但被中断的任务仍标记为不确定, 不自动重放. 启动时取消之前已入库但未提交的普通消息, 恢复会话不等于继续执行任务. 决定重发前先检查会话历史和工作区副作用. 待处理控制命令仍经过正常鉴权. `/resume` 保留为手动选择原生会话的方式, 不通过 `--continue` 猜测, 不解析终端输出.
- 普通任务的全部最终文本分段和 inbox `done` 在同一 SQLite 事务提交, 持久化失败则整体回滚. 已提交的 outbox 结果不因后续会话切换而取消. inbox/outbox 的 pending 查询使用 `(state,id)` 索引.
- Telegram client 区分交付确定性: 明确的本地发送前失败及完整 API 拒绝记为 `failed`, 可能已交付但无法确认的记为 `uncertain`. 两者均不新增自动重发. 保留既有明确限流的有界重试, 保留的附件快照需人工处理.
- 正常退出清理 omp 进程组. Linux 的 RPC/ACP 启动还使用父死亡 SIGTERM, 并保持创建子进程的 OS 线程存活至唯一的进程等待完成. 每个存活子进程占用一个锁定的 OS 线程. 这不是整个进程树 containment: 后代进程, 忽略 SIGTERM 或清除父死亡信号的程序仍需外部 containment 兜底. 不强制部署模板.
- Telegram 发送与本地数据库提交不是同一个事务. 网络超时可能已经送达; 不承诺 exactly-once, 不能把没有收到回复当成工具未执行.
- 独立 omp 进程不等于文件系统或凭据隔离. 多个 topic 使用同一工作目录时会共享文件. 并发修改需要隔离时, 选择不同目录或自行准备 Git worktree. 桥接不创建 worktree, 不清空项目内容, 不删除工作目录.
- omp 继承 daemon 的用户权限和环境, 包括环境中的 token/模型凭据. 应使用低权限专用用户, 限制目录和凭据访问, 只授权可信操作者. 白名单不是工具执行沙箱.
- Telegram 群成员可能看到请求, 回答和按钮文本, 即使他们不在操作白名单. 不要发送秘密. 桥接数据库保存会话绑定及收到/发出的消息内容, 不是 omp 完整模型上下文. 已完成消息记录目前没有自动清理, 应保护数据库. 不分享原始 RPC/get_state 输出, 其中可能含认证 header 和 system prompt.

## 当前边界与验证状态

支持已有 topic 内的文字, 原生图片和文档附件收发. 不支持语音和自动创建 topic. 不做 PTY/ANSI 终端模拟.

已在本机 omp 18.2.1 验证两个真实进程的 session ID/路径隔离, 一次真实模型请求返回 `RPC_OK`, terminal `agent_end`, 关闭后按原路径恢复相同 session 和消息数, 以及另一个会话保持为空. CLI `--check` 已实际运行通过.

本地协议夹具覆盖 topic 并发路由, 忙时排队与 stop, new 确认的用户归属和重复点击, close/resume, compact 后继续对话及敏感状态过滤. SQLite 测试覆盖接收/offset 原子性, 重启时提交和发送状态不确定化, 不自动重放及会话历史保存. Telegram HTTP 测试覆盖 429, 网络错误, 消息格式和凭据脱敏.

动态工作目录已通过真实 bridge 和两个实际 omp 进程验证, 仅 Telegram 传输使用模拟: 独立目录, 模型返回 `WORKSPACE_OK`, 恢复原目录/session, 确认 `/new` 后保留旧文件. 实际 CLI 也验证了工作目录环境变量未设置, 空值和指定路径三种情况. 这些检查没有通过 Telegram 发送消息.

自动重启恢复已通过真实 omp 模型回复及 daemon 停止/重开验证: 运行中的 topic 恢复准确的原生 session 文件和工作目录, 已关闭 topic 保持关闭. 此检查的 Telegram 传输使用模拟. 协议夹具另外覆盖中断任务不重放, 取消待提交消息, `/stop`, chat 授权移除, 会话缺失及 worker 上限降低.

可靠性验证注入最终回复分段写入失败, 确认无残留分段和错误完成状态; 成功提交后重开 SQLite 仍按顺序交付. 查询计划使用两个 state 索引. 本地 HTTP 验证区分完整 API 拒绝, 不确定响应及本地附件缺失. 真实 omp 的父进程 SIGKILL 实验观察到 omp, shell, tool 和普通孙进程均退出; 隔离 RPC/ACP 夹具同时证明不配合清理及脱离进程组的后代仍可存活. 不据此承诺任意进程树退出. 启动意图事务仅完成设计, 见[设计评审](omp-telegram-daemon-design.md)第 27 节.

已通过真实 Telegram `getMe` 和 `getWebhookInfo` 验证 bot 连接, 私聊 topics 能力和无 webhook 冲突. 真实文件上传/下载已逐字节核对, 图片上传/下载获得了可解码图片. 另一次真实 omp 配合模拟 Telegram 传输的测试识别了上传图片颜色, 读取了文档, 并调用 `telegram_send` 回传两份原件. **从手机发送附件的完整对话, 多 topic 故障场景和真实工具审批弹窗仍需端到端验收.** 这些独立检查不代表完整线上界面已验证.

```sh
just check
```
