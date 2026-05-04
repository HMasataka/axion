# 仕様: Axion v0.2 — サーバー/クライアント方式への転換

## 技術コンテキスト

v0.1 の P2P 対称構造（カスタム TCP プロトコル + メモリ全量読み込みのファイル転送）を完全に廃止し、サーバー/クライアント方式へ転換する。サーバーは Web UI を `embed.FS` で配信し、クライアント一覧と同期ペアを SQLite (`modernc.org/sqlite`、純 Go) で永続化する。クライアントは起動時に UUIDv4 を `~/.config/axion/client.id` に保存し、`os.Getwd()` を root とする root jail 内のファイルのみアクセス可能。サーバー←→クライアントは PSK で認証された WebSocket（制御プレーン）と HTTP (`PUT/GET /v1/blobs/{sha256}` + Range ヘッダ、データプレーン）の二本立てで通信する。Web UI は単一管理者向け HTTP Basic 認証 + html/template + HTMX + pico.css。同期セマンティクスは双方向 = LWW（サーバー受信時刻基準）+ コンフリクトファイル退避、片方向 = strict mirror（src→dst、dst 側変更は次回上書き）。スコープは「クライアント登録／2クライアント間ペア CRUD／同期方向選択／ListDir」までに絞り、3 者以上同期・直接 P2P・自動 TLS・マルチユーザー・Prometheus メトリクスは v0.3 以降に送る。`internal/watcher` のみ流用、`internal/protocol` と `internal/peer` は完全廃棄。v0.1 との互換性なし。macOS / Linux 公式サポート、Windows は best-effort。

## 機能要件

### サーバー

- [x] `axion server` サブコマンドを新設し、CLI で `--bind`（デフォルト `127.0.0.1:8765`）、`--data-dir`（SQLite と blob 保存先、デフォルト `~/.local/share/axion`）、`--admin-user`/`--admin-password`（Web UI Basic 認証）、`--psk-file`（クライアント認証用 PSK のパス、デフォルト `~/.config/axion/server.token`）を受け付ける
- [x] サーバー起動時に SQLite (`modernc.org/sqlite`) を初期化し、`embed.FS` 同梱の `migrations/*.sql` を `schema_version` テーブルベースで順次適用する
- [x] サーバーは PSK を Bearer トークンとした WebSocket エンドポイント `/v1/ws` を提供する
- [x] サーバーはクライアントから `RegisterRequest{ClientID, Hostname, RootPath, Version, ProtoVersion}` を受け、`clients` テーブルに upsert する（`first_seen`/`last_seen` を更新）
- [x] サーバーは WebSocket ping/pong を 10 秒間隔で送信し、pong 不達 30 秒で該当クライアントを `offline` に遷移させる
- [x] サーバーはクライアントから受信した `FileChangedEvent` に対して `server_mod_time = time.Now().UnixNano()` を打刻し、`file_state` テーブルを更新したうえで反対側クライアントに `FileSyncCommand` をプッシュする
- [x] サーバーは `PUT /v1/blobs/{sha256}` でクライアントからアップロードを受け、`<data-dir>/blobs/<sha[0:2]>/<sha>.partial` に書き、SHA を検証してから atomic rename する
- [x] サーバーは `GET /v1/blobs/{sha256}` で `Range` ヘッダ対応のダウンロードを提供する
- [x] サーバーは Web UI 経由の `GET /v1/clients/{id}/listdir?path=...` を受け、対象クライアントへ WS で `ListDirRequest` を送って応答を集約する
- [x] サーバーは再起動後、`sync_pairs` を SQLite から復元し、再接続したクライアントとの同期を自動再開する
- [x] サーバーは `audit_log` テーブルにペア作成/編集/削除、コンフリクト発生、削除vs編集競合などのイベントを記録する

### クライアント

- [x] `axion client` サブコマンドを新設し、CLI で `--server`（サーバー URL）、`--root`（同期可能ディレクトリの上限、デフォルト `os.Getwd()`）、`--id-file`（クライアント ID ファイル、デフォルト `~/.config/axion/client.id`）、`--psk-file`（PSK パス）を受け付ける
- [x] クライアントは起動時に `--id-file` が無ければ UUIDv4 を生成して保存し、サーバーへの WS ハンドシェイクで Bearer PSK と `RegisterRequest` を送信する
- [x] クライアントの全ファイル I/O は `internal/clientfs/jail.go` の `Jail` ヘルパー経由とし、`os.Open*` / `os.ReadFile` / `os.WriteFile` の直接利用を `golangci-lint` の `forbidigo` で禁止する
- [x] クライアントは `Jail.Resolve(rel)` で `filepath.Clean` → `filepath.EvalSymlinks` → `filepath.Rel` による prefix 検証を行い、root 外を指す要求は `ErrPathEscape` で拒否する
- [x] クライアントは `internal/watcher` を流用し、root 配下のファイル変更を fsnotify で監視、200ms デバウンスで `FileChangedEvent` を WS でサーバーへ送る
- [x] クライアントはサーバーから受けた `FileSyncCommand` に基づき `GET /v1/blobs/{sha256}` でストリーム取得し、`<rel>.partial` 経由 atomic rename で書き込む
- [x] クライアントは originator_client_id 由来の自身への書込み中、watcher イベントを suppress してループを抑止する
- [x] クライアントは WS 切断時に exponential backoff（1→2→4→...→60s、±20% jitter）で再接続する
- [x] クライアントは再接続時、サーバーからの `ListFilesRequest{PairID, Side}` に対し `(rel_path, sha256, mod_time)` のスナップショットを返し、サーバー側で差分検出される
- [x] クライアントは SIGTERM/SIGINT で graceful shutdown し、サーバー側 `last_seen` を即時更新する

### Web UI

- [x] Web UI は `embed.FS` 同梱の html/template + HTMX + pico.css で配信され、Go バイナリ単体で動作する
- [x] `/` ページにクライアント一覧（display_name / hostname / root_path / status (online/offline) / last_seen）を表示する
- [x] 各クライアント行に「display_name 編集」ボタンを設け、Web UI から人間可読な名前を付与・編集できる（DB の `clients.display_name` カラムに保存）
- [x] `/pairs` ページに同期ペア一覧と「新規作成」ボタンを表示する
- [x] 同期ペア作成フォームでは、クライアント A・パス A（ListDir でブラウズ可能なディレクトリツリーから選択）、クライアント B・パス B、同期方向（A→B / B→A / 双方向）、ペア名、有効/無効を選択する
- [x] `/clients/{id}/browse` で対象クライアントの root 配下のディレクトリツリーをブラウズできる（ListDir 経由、ファイルは表示のみで操作不可）
- [x] `/runs` ページに `sync_runs` の実行履歴（ペア名 / 開始終了時刻 / 状態 / バイト数 / ファイル数 / コンフリクト数 / エラーメッセージ）を一覧表示する
- [x] Web UI は HTTP Basic 認証で保護され、フォーム送信は CSRF double-submit cookie で検証される
- [x] 同期ペアの作成・編集・削除はページリロード不要で HTMX による差分 swap で反映される
- [x] クライアントの online/offline 状態変化は WebSocket push (`hx-ws`) で UI に自動反映される

### 同期セマンティクス

- [x] 双方向同期では、サーバー受信時刻基準の Last-Writer-Wins で勝敗を決し、負け側を `<name>.conflict-<client_id_short>-<unix_ns>` にリネーム保存する
- [x] 片方向同期 (A→B、strict mirror) では B 側の独立変更は無視され、次回 A→B 同期時に上書きされる（B 側で削除したファイルも A から復活する）
- [x] 削除 vs 編集の競合では「編集が勝つ」（削除を取り消し）を採用し、`audit_log` に記録、Web UI で警告バッジを表示する
- [x] 同期対象は通常ファイルとディレクトリのみとし、symlink / FIFO / socket / デバイスファイルはスキップして `audit_log` に記録する
- [x] 巨大ファイル転送中にクライアント切断が発生した場合、`.partial` ファイルが残り、次回再開時にレジューム可能（HTTP Range）

### プロトコル

- [x] WebSocket メッセージ枠は 64KB を上限とする（`SetReadLimit`）
- [x] HTTP ファイル転送は 1MB チャンクで `Content-Range` 対応、ストリーム I/O (`io.Copy`) でメモリ占有を最小化する
- [x] WS / HTTP いずれも `proto_version` ハンドシェイクを行い、不一致時はサーバー側で接続を拒否し Web UI で警告表示する
- [x] ファイル単体の上限は 1GB（設定で 10GB まで拡張可）、サーバー全体ストレージ上限は 10GB/クライアント

## 非機能要件

- [x] サーバー単一バイナリで起動可能（CGO 不要、`go build` ワンコマンド）
- [x] Web UI および全静的アセットは `embed.FS` でバイナリに同梱する
- [x] 構造化ログは既存の `log/slog` を使用し、`client_id` / `pair_id` / `rel_path` / `sha256[0:8]` を共通フィールドとする `logctx.With(ctx, ...)` ヘルパーを導入する
- [x] サーバーのデフォルトバインドは `127.0.0.1`、`--bind 0.0.0.0` は明示オプトインとする
- [x] TLS は v0.2 では「外部 reverse proxy（nginx / caddy 等）に委譲」する前提とし、サーバー本体は HTTP のみを提供する
- [ ] クライアントは macOS / Linux で公式サポートし、Windows は best-effort（パス区切り・大文字小文字差異は警告のみ）
  - 可能であればwindowsもサポートしたい
- [x] SQLite スキーマ変更は `migrations/NNNN_xxx.sql` を追加することで適用され、`schema_version` テーブルでバージョン管理する
- [x] サーバーへの単一クライアント同時同期数は 4 ジョブまで、サーバー全体で 64 ジョブまでとし、超過時はキューイングする
- [x] 同期最大ファイル数 100,000、最大ディレクトリ深度 32 を超えたら警告ログを出力する

## エッジケース・リスク

- [x] 同名フォルダ衝突は Web UI 上「display_name + 絶対パス」併記で識別する
- [x] 同一クライアントが複数ペアに所属する場合、`originator_client_id` + 同期中フラグでループ伝播を抑止する
- [x] 同一ファイルが両側でほぼ同時に変更された場合、サーバー受信時刻順で LWW を適用し、負け側をコンフリクトファイルとして保存（データロス無し）
- [x] クライアント間の時計ずれはサーバー受信時刻に統一することで吸収する
- [x] 巨大ファイルの転送中切断は `.partial` テンポラリ + atomic rename + HTTP Range レジュームで対応する
- [x] fsnotify が空ディレクトリ作成を取りこぼすケースは、起動時/再接続時の差分スキャンでカバーする
- [x] macOS（case-insensitive）⇄ Linux（case-sensitive）でのファイル名衝突はペア作成時にプラットフォーム差異警告を Web UI で表示する
- [x] WebSocket プロキシ越し（nginx 等）でのバッファリング切断対策として keepalive ping と適切なタイムアウト値をドキュメント化する
- [x] Web UI 同時編集対策として楽観ロック（`If-Match: <etag>`）をペア更新 API に導入する
- [x] `proto_version` 不一致時は登録ハンドシェイクで拒否し、Web UI に警告表示する
- [x] サーバー側ディスクフル時は 5xx を返してクライアントに backoff を指示する
- [x] 攻撃者が長さフィールドに巨大な値を指定する DoS は WebSocket `SetReadLimit(64*1024)` と HTTP `Content-Length` 検証で防ぐ
- [x] `..` を含む相対パスや root 外シンボリックリンクは `Jail.Resolve` で 400 エラーで拒否し `audit_log` に記録する
- [x] ignore_list は v0.2 ではグローバル設定のみ（Web UI で編集可）、ペア個別 ignore は v0.3 以降

## Open Questions

- [ ] クライアント登録時に管理者の手動承認を必須とするか（v0.2 ではスコープ外として「PSK 一致 = 自動承認」とする方針だが、明示確認）
  - スコープ外
- [x] `display_name` が未設定の場合、UI 上のフォールバック表示は「hostname / root_path 末尾」とするか「UUID 先頭 8 文字」とするか
  - hostname
- [x] サーバー側 blob ストアの GC 戦略（参照されなくなった blob をいつ削除するか、保持期間 / 容量上限到達時のいずれか）
  - 一定時間経過 時間は設定可
- [x] ペア無効化（enabled=0）時の挙動 — 監視のみ停止か、設定保持して再開時に差分スキャンを行うか
  - 監視のみ停止 + `file_state` 保持。再有効化時にクライアントが差分スキャンを行い、無効化中の変更を反映する（選択肢A）
- [x] 既存設定ファイル `~/.config/axion/config.json` (v0.1) との共存方針 — 起動時に検出して案内メッセージを出すか、無視するか
  - 無視 サーバーonly
- [x] グローバル ignore_list のデフォルト値を v0.1 から引き継ぐか（`.git`, `.DS_Store`, `Thumbs.db`, `*.tmp`, `*.swp`, `*~`）
  - サーバーデフォルトとして引き継ぎ
- [x] サーバー停止中に発生したクライアント側変更の扱い — 再接続時の差分スキャンでカバーされる前提だが、サーバー停止が長時間（>24h）続いた場合のクライアント側 backoff 上限
  - 差分スキャンでカバー
- [x] CSRF トークンの有効期限と更新タイミング（セッション中固定 vs リクエスト毎）
  - セッション中固定
