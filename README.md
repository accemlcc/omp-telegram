# omp-telegram

[中文版](README.zh.md)

A standalone Go daemon connecting existing Telegram topics to `omp --mode rpc`. Each topic gets an independent omp process and session. Ordinary text prompts run sequentially within a topic, while different topics can run concurrently. See [PLAN.md](PLAN.md) for the design and acceptance scope, and [AGENTS.md](AGENTS.md) for development guidelines.

## Requirements

- Linux or Linux under WSL2. The daemon uses Unix process groups and file locks; native Windows execution is not supported.
- Go 1.26 or later, [just](https://github.com/casey/just), and access to Go modules for building. SQLite uses a pure Go driver.
- A working omp installation and its runtime dependencies, with models and authentication configured for the service user. RPC must support the expected ready/framing protocol and v2 negotiation. Incompatible versions are rejected; there is no terminal-emulation fallback.
- Network access to the Telegram Bot API and the selected model provider. Run only one polling instance per bot, with no active webhook or competing getUpdates consumer.

## Telegram setup

1. Create a bot through the official `@BotFather` and keep its token private.
2. Prefer a supergroup with Topics enabled. Have an administrator create a topic, add the bot to the group, and allow it to send messages. The daemon does not create or reopen topics, so topic-management privileges are not required for this purpose.
3. The bot must receive ordinary text messages. Disable privacy mode through BotFather's `/setprivacy` and re-add the bot if Telegram instructs you to, or grant administrator status if needed. Prefer minimal permissions. Receiving slash commands alone is insufficient for text conversations.
4. Add your numeric user ID and the target numeric chat ID to the allowlists. Group chat IDs are usually negative. Before starting the daemon, use a Bot API client you control to inspect `message.from.id` and `message.chat.id` in getUpdates. Do not give the token to third-party ID lookup websites. Send messages as your personal account, not as an anonymous administrator or a channel.
5. Inside a topic, send `/new test` to start a fresh omp session directly in `<workspace_root>/test`, or `/new /tmp/test` to work directly in `/tmp/test`. Missing directories are created; existing files are not copied or cleared. A bound topic can omit the argument to reuse its directory. `/resume <omp-session-id>` restores an existing native session and its original directory. Private chats require Telegram's threaded mode.

## Build, install, and run

```sh
just
chmod 600 config.toml
```

`just` and `just build` compile `./omp-telegram`. `just install` builds and installs only the binary to `~/tool/omp-telegram/omp-telegram`; use `just install /your/bin/directory` to choose another directory. Configuration and data are not copied. `just test` depends on `install`, then runs `supervisord ctl restart omp-telegram`, using your existing go-supervisor configuration. Configure that service to run the binary in the default installation directory. Service arguments and environment come from supervisor, not the invoking shell. `just check` runs unit tests, race checks, and vet without Telegram polling. Deployment remains user-managed.

Use `--config <path>` or `-c <path>` to select a configuration file, and `--check` to validate it without starting the service. For example: `./omp-telegram -c config.local.toml --check`.

Without `--config`, the bridge first reads `config.toml` next to the real executable after resolving symlinks. If that file is absent, it uses the repository's `config.toml` embedded at build time, without creating a configuration file. Existing unreadable or invalid files still fail, as does a missing explicit `--config` path. The bundled configuration requires `OMP_TELEGRAM_BOT_TOKEN`, `OMP_TELEGRAM_ALLOWED_USERS`, and `OMP_TELEGRAM_ALLOWED_CHATS` in the process environment. Installing elsewhere still changes the default data and workspace locations; existing data is not migrated. For foreground operation, set the required environment variables and run `./omp-telegram`; use `./omp-telegram --check` to validate configuration, or `./omp-telegram --config config.local.toml` for private settings. Explicit relative configuration paths are caller-relative. Do not run foreground and supervisor instances with the same bot or database simultaneously. Ctrl-C stops a foreground instance. No credentials are written to the tracked template, and no static project list is needed.

| Field | Meaning |
| --- | --- |
| `token` | Bot token; prefer `"${OMP_TELEGRAM_BOT_TOKEN}"` rather than a literal secret. Defaults to that reference when omitted |
| `allowed_users`, `allowed_chats` | Numeric ID arrays; both allowlists must match. There is no automatic pairing |
| `workspace_root` | Root for dynamically created workspaces; defaults to `OMP_TELEGRAM_WORKSPACE_ROOT`, or `workspace/` under the real executable directory when that variable is unset or empty |
| `omp` | Defaults to `"omp"`, resolved through `PATH`; an executable name or path, not a shell command with arguments |
| `omp_args` | Optional quoted argument string; defaults to `${OMP_TELEGRAM_ARGS}`. Unset or empty means no additional arguments |
| `data_dir` | Defaults to `.`: `omp-telegram.db` and `daemon.lock` live directly under the real executable directory |
| `max_workers` | Maximum number of simultaneously running omp processes |
| `queue_capacity` | Capacity of each topic's waiting text-prompt queue |

Relative workspace roots and the data directory resolve against the real executable directory, not the launch working directory or configuration file's directory. Absolute values are unchanged. Two daemons cannot use the same data directory, and different bots must not share one.

By default, bridge configuration, state, and workspaces stay next to the executable: `config.toml`, `omp-telegram.db`, `daemon.lock`, and `workspace/`. This is bridge configuration, not omp configuration. Unless you explicitly supply startup options through `omp_args`, omp keeps its own model, authentication, approval, extension, and history defaults. The bridge does not automatically pass `--session-dir`, relocate history, or maintain a second model transcript. It stores only the session path returned by omp for resuming. SQLite may also create `omp-telegram.db-wal` and `omp-telegram.db-shm` while running; do not delete them or copy only the main database during active writes. Database files remain private; existing project-directory permissions are not changed.

String values support `$VAR` and `${VAR}` environment references. Use `$$` for a literal dollar sign. Expansion happens once after TOML parsing and cannot inject TOML fields. Missing variables normally fail loading. An exact reference to `OMP_TELEGRAM_WORKSPACE_ROOT` in `workspace_root` falls back to executable-directory `workspace/` when unset or empty; omitting the field uses that reference, and an explicit empty field uses the same fallback. `OMP_TELEGRAM_ARGS` in `omp_args` is also optional, as described below. Explicit nonempty settings take precedence. Shell defaults such as `${VAR:-fallback}`, command substitution, and automatic `.env` loading are not supported.

IDs and limits accept either TOML integers or quoted decimal strings. Allowlist string elements also accept comma-separated IDs, including values expanded from environment variables; surrounding whitespace is ignored, but empty or invalid entries fail loading. Limits still require a single integer. Quote references; do not write unquoted `${VAR}` in TOML. Keep allowlists inside TOML arrays as shown below:

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

Export each referenced variable before starting the daemon, or supply it through your chosen process manager. The distributed example requires `OMP_TELEGRAM_BOT_TOKEN`, `OMP_TELEGRAM_ALLOWED_USERS`, and `OMP_TELEGRAM_ALLOWED_CHATS`. The example above additionally requires `OMP_TELEGRAM_MAX_WORKERS`. For multiple users/chats, set values such as `OMP_TELEGRAM_ALLOWED_USERS=123456789,987654321` and `OMP_TELEGRAM_ALLOWED_CHATS=123456789,987654321,-1001234567890`, or use literal TOML arrays. Private chat IDs match the corresponding user IDs; group chat IDs are negative. Authorization requires membership in both global allowlists; these are not per-user chat assignments.

`/new` creates a new omp process and conversation, not a new filesystem identity. `/new test` uses `<workspace_root>/test` directly; `/new /tmp/test` uses `/tmp/test` directly. Absolute and `~`/`~/...` paths may be missing: the bridge resolves existing ancestor symlinks and creates the requested directory. Existing directories and files remain in place. Simple names must be one component, not `.`, `..`, a nested relative path, or a control-character string; named-directory symlinks and dangling links are rejected. No path-derived group or random ID directory is added.

An unbound topic must supply a name/path for `/new`. A bound topic's argument-free `/new` starts a fresh native session in the exact saved directory, even if `workspace_root` has changed. Replacing a running instance requires confirmation. Different topics can explicitly choose the same directory: conversations remain separate, but files are shared and concurrent edits can conflict.

Use `/resume` to choose a native omp session in this topic's working directory. A short-lived `omp acp` process supplies the list through `session/list`; the bridge neither scans session files nor substitutes its database history. The picker shows eight sessions per page, with titles, IDs, timestamps, Previous/Next, and Cancel. Selection replaces an idle instance; active tasks, compaction, or queued prompts block switching. Old, expired, and other-user callbacks cannot switch sessions. A topic without a directory must first use `/new <name or path>` or the explicit ID shortcut.

`/resume <omp-session-id>` remains a shortcut, including from an unbound topic; close an existing instance first. A hexadecimal prefix of at least eight characters is accepted by native lookup; prefer the full ID. omp restores the session and its recorded directory. The bridge reads RPC identity and local `/session info` metadata, saves the native session-file path, and shows the native ID in the ready message and `/status`. One native session cannot run in multiple bridge topics. Missing original directories fail rather than being recreated. An unused new session may not be resumable until omp persists its history.

### Explicit omp startup arguments

To use an omp configuration overlay specifically for Telegram, keep this in the bridge's `config.toml`:

```toml
omp_args = "${OMP_TELEGRAM_ARGS}"
```

Set the environment variable in your shell, quoting paths containing spaces:

```sh
export OMP_TELEGRAM_ARGS="--config \"$HOME/.config/omp/telegram.yml\""
```

Or configure the string directly instead:

```toml
omp_args = '--config "${HOME}/.config/omp/telegram.yml"'
```

Create that file yourself using omp's configuration format. The bridge only passes its path to omp; it does not create or modify omp configuration files. Options such as `--profile` or `--model` can also be supplied explicitly. These arguments apply whenever a worker starts, including `/resume`; restart the daemon to load changed configuration or environment variables.

The string is tokenized using shell-style quotes and backslash escapes, then passed directly as argv. No shell, command substitution, globbing, or `~` expansion runs. Values obtained from `OMP_TELEGRAM_ARGS` are not recursively environment-expanded: expand `$HOME` in your shell as shown above, or supply an actual absolute path. Unlike bridge data paths, relative paths in omp arguments are interpreted by omp in the worker's working directory. Use absolute paths for configuration overlays.

Omitting `omp_args` defaults to the optional `OMP_TELEGRAM_ARGS` variable; unset or empty means no additional arguments. Explicit `omp_args = ""` disables extra arguments even if the environment variable is set. Malformed quotes fail configuration loading without logging argument values.

RPC mode, working directory, and session lifecycle remain bridge-managed. `--mode`, `--cwd`, `--resume`/`--session`/`-r`, `--continue`/`-c`, `--print`/`-p`, `--no-session`, and the `--` separator are rejected in `omp_args`. Other explicit options are interpreted by omp, including their validation and permission effects.

Read the token interactively in Bash to avoid putting its value in shell history:

```sh
export OMP_TELEGRAM_ALLOWED_USERS=123456789
export OMP_TELEGRAM_ALLOWED_CHATS="$OMP_TELEGRAM_ALLOWED_USERS"
read -r -s -p 'Telegram bot token: ' OMP_TELEGRAM_BOT_TOKEN; printf '\n'
export OMP_TELEGRAM_BOT_TOKEN
just build
./omp-telegram --check
./omp-telegram
```

`./omp-telegram --check` validates configuration without starting Telegram polling: it parses TOML, expands references, checks startup arguments and the omp executable, and creates the data directory and workspace root. Referenced variables must be set except for the optional workspace root and omp arguments. It does not validate model authentication or parse omp's own configuration file. For a supervisor-managed instance, set the environment in supervisor and use `just test` to rebuild and restart it.

## Topic commands

The Telegram interface is English-only: slash-command descriptions, `/help`, status, confirmation buttons, progress labels, and bridge errors use English regardless of client language. The daemon registers the English command list at startup and replaces its previously registered Chinese list with English too. Type `/` in a topic to see suggestions; reopen the chat if Telegram cached an older menu. Registration failures stop startup with a sanitized error. User messages, model replies, and extension-provided dialog content are not translated. This README and README.zh.md remain bilingual documentation. Menu visibility does not grant permission to execute commands.

| Command | Behavior |
| --- | --- |
| `/new [name or path]` | Start a fresh omp session directly in the selected directory, creating it if missing; omit the argument later to reuse the exact directory. Existing files remain intact |
| `/stop` | Request cancellation of the current task and clear waiting text prompts, keeping the session |
| `/close` | Close the instance and clear waiting text prompts, preserving history |
| `/resume [omp-session-id]` | Without an ID, choose a native session in the current directory using paginated buttons; with an ID, restore it directly after closing the current instance |
| `/status` | Show working directory, native session ID, model, activity and queue information; when not running, report the global count of uncertain records |
| `/model` | Show the current status and model |
| `/model provider/model` | Switch models while idle |
| `/compact` | Compact context while idle, after button confirmation |
| `/help`, `/start` | Show help |

Ordinary text goes only to an already running instance; text never starts a process automatically. `/command@botname` is supported. Unknown slash commands are not forwarded to omp. Output uses throttled previews and split final messages, without forwarding raw RPC state or full tool output.

RPC `confirm` and a bounded number of `select` options use buttons and are canceled on timeout. These can represent permission requests only when omp exposes them through RPC; this is not a mirror of every terminal permission dialog. `input`/`editor` dialogs are unsupported and explicitly canceled. The bridge never enables automatic approval. It does not modify omp configuration files or override model/credential/approval settings. It registers the bridge-owned `telegram_send` RPC host tool for the attachment transport described below; other configuration changes still require explicit `omp_args` or Telegram commands such as `/model`.

## Images and files

Use Telegram's normal photo or attachment button inside a topic with a running omp instance. No upload command is needed. The caption becomes the prompt; without a caption, the bridge asks omp to inspect the attachment. Captions are treated as prompt text, not bridge slash commands. Albums are handled as separate incoming messages, sequentially within the topic.

Original files are retained in the current workspace under `.telegram/incoming/`. Filenames are sanitized, and download paths are confined to that workspace. Supported JPEG/PNG/GIF/WebP images are included in the RPC prompt. The inline budget is 512 KiB: larger images get a bounded JPEG preview while the original remains available by local path. Images over the 16-million-pixel decoding limit, unsupported formats, and non-image documents are supplied by local path with an explicit note, not misrepresented as an inline image. Image interpretation requires a capable model; reading local documents depends on omp's configured tools.

Originals submitted to omp are kept for the session. Failed or canceled preparation is cleaned up; stopping a task does not delete files that were already submitted to omp.

For replies, ask naturally, such as "send me the report" or "send that image back". omp can call `telegram_send` with `path`, optional `kind` (`document` by default or `photo`), and optional `caption`. No `/sendfile` command is required. The tool accepts only regular files inside the current workspace and can run only during an active Telegram request. It cannot choose another chat/topic. Source files are copied to private snapshots under `<data_dir>/attachments/outbox/` before durable enqueueing. A tool success means queued, not confirmed delivery; confirmed uploads remove their snapshots, while uncertain deliveries retain them and are not automatically replayed.

With the hosted Telegram Bot API, downloads are capped at 20 MB, documents at 50 MB, and photos at 10 MB. Outgoing photos must be JPEG/PNG with Telegram-compatible dimensions; use document mode for originals or other formats. Captions are limited to 1024 UTF-16 code units. Download/preparation is asynchronous and bounded so `/stop` is not stuck behind a file transfer. Each transfer has a five-minute request timeout. Authorization runs before any file download. The database stores attachment metadata and delivery state, not binary file contents.

Voice messages, transcription, and automatic topic creation are not supported. The bridge does not scan directories or automatically upload every generated file.

## Recovery and security boundaries

- At startup, the daemon automatically restores topics whose instances were still running at shutdown, using their exact saved native session-file paths and working directories. `/close` disables automatic restoration; `/stop` keeps the instance eligible. Recovery is scoped to the current bot and allowed chats and respects `max_workers`. Missing sessions/directories or capacity limits leave the saved identity and restoration intent intact; use `/resume` after resolving the problem, or retry on the next service restart. No replacement session is silently created.
- Database schema version 1 is recorded in SQLite's `PRAGMA user_version`. New empty databases create the current schema and version in one transaction. This development version does not migrate older schemas: populated unversioned databases and unsupported versions are rejected without changing their schema or records. Back up incompatible databases and use a fresh data directory; do not merely change the version number. Post-release schema changes will require explicit transactional migrations between versions.
- Interrupted submitted tasks remain uncertain and are never automatically replayed. Previously persisted pending ordinary prompts are canceled at startup; restoration resumes the conversation, not the work. Inspect session history and workspace side effects before deciding to resend a request. Pending control commands still pass through normal authorization. `/resume` remains available for manual native session selection; the bridge never guesses with `--continue` or parses terminal output.
- Final text reply parts and the ordinary task's inbox `done` state commit in one SQLite transaction. A persistence failure rolls back the whole completion. Already committed outbox results are not canceled by later session changes. Pending inbox/outbox queries use `(state,id)` indexes.
- Delivery errors are classified in the Telegram client: proven local pre-send failures and complete explicit API rejections become `failed`; possible delivery without confirmation becomes `uncertain`. Neither state adds automatic resend. Existing bounded retries for explicit rate limits remain; retained attachment snapshots require manual handling.
- Normal shutdown cleans the omp process group. On Linux, RPC and ACP launches also use parent-death SIGTERM, with the creating OS thread kept alive until the sole process wait completes. This costs one locked OS thread per live child and is not process-tree containment: descendants, ignored SIGTERM, or programs clearing the parent-death signal still need an external containment boundary. No deployment template is imposed.
- Telegram delivery and local database commits are not one transaction. A timed-out message may already have arrived. There is no exactly-once guarantee, and a missing reply does not prove that tools did not execute.
- Separate omp processes do not isolate filesystems or credentials. Topics using the same working directory share its files. Choose separate directories or prepare your own Git worktrees when concurrent edits must be isolated. The bridge does not create worktrees, clear project contents, or delete working directories.
- omp inherits the daemon user's permissions and environment, including any bot token and model credentials. Use a dedicated low-privilege account, restrict filesystem and credential access, and authorize trusted operators only. Allowlists are not a tool-execution sandbox.
- Telegram group members may see requests, answers, and button text even if they are not authorized operators. Do not send secrets. The bridge database stores session bindings and inbound/outbound message content, not omp's full model transcript. Completed message records currently have no automatic pruning; protect the database. Never share raw RPC/get_state output, which may contain authentication headers and system prompts.

## Current limitations and verification status

Text, native photos, and document attachments are supported in existing topics. Voice and automatic topic creation are not supported. There is no PTY/ANSI terminal emulation.

Local verification with omp 18.2.1 covered distinct session IDs and paths for two real processes, one real model request returning `RPC_OK`, a terminal `agent_end`, restoration of the same session and message count after closing and resuming by path, and the other session remaining empty. The CLI `--check` command was also exercised successfully.

Local protocol fixtures cover concurrent topic routing, queueing while busy and stop behavior, ownership and repeated clicks for new-session confirmations, close/resume, continued conversation after compaction, and sensitive-state filtering. SQLite tests cover atomic input/offset persistence, conversion of submitted tasks and in-flight deliveries to uncertain state on restart, no automatic replay, and session-history preservation. Telegram HTTP tests cover 429 responses, network errors, message formatting, and credential redaction.

Dynamic workspace verification also exercised the real bridge with two actual omp processes and a simulated Telegram transport: distinct directories, a real `WORKSPACE_OK` model reply, restoration of the original directory/session, and a confirmed `/new` preserving old files. Actual CLI checks covered unset, empty, and explicit workspace-root environment values. These checks did not send messages through Telegram.

Automatic restart recovery was exercised with a real omp model reply and daemon shutdown/reopen: the live topic resumed the exact native session file and working directory, while a closed topic stayed closed. Telegram transport was simulated for this check. Protocol fixtures additionally cover interrupted-task non-replay, canceled pending prompts, `/stop`, removed chat authorization, missing sessions, and reduced worker limits.

Reliability checks injected a failed final-reply part and confirmed zero partial output and no false completion; successful completion retained ordered replies after reopening SQLite. Query plans use both state indexes. Local HTTP checks distinguish complete API rejection from ambiguous responses and missing local attachments. A real omp parent-SIGKILL experiment observed omp, shell, tool, and an ordinary grandchild all exit; isolated RPC/ACP fixtures also demonstrate that non-cooperating and escaped descendants can survive. These observations do not establish a universal tree-cleanup guarantee. The startup-intent transaction remains design-only in section 27 of [the design review](omp-telegram-daemon-design.md).

Real Telegram `getMe` and `getWebhookInfo` calls verified bot connectivity, private topic capabilities, and no webhook conflict. Live document upload/download preserved exact bytes, and live photo upload/download returned a decodable image. A separate real-omp run with simulated Telegram transport recognized an uploaded image's color, read an uploaded document, and invoked `telegram_send` to return both originals. **A phone-originated attachment conversation, multi-topic fault scenarios, and real tool-approval dialogs still need end-to-end acceptance.** These distinct checks are not a claim of complete live UI coverage.

```sh
just check
```
