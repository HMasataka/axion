# ロードマップ: Axion v0.2 — サーバー/クライアント方式への転換

## アーキテクチャ方針

v0.1 の P2P 対称構造を完全廃棄し、**サーバー集約型ハブ＆スポーク構造**へ転換する。クライアントは「watcher + jail されたファイル I/O + WebSocket クライアント + HTTP blob 転送」のみを担う薄いエージェントとし、サーバーが状態（SQLite, `modernc.org/sqlite`）・ルーティング（WS Hub）・blob 中継（HTTP PUT/GET + Range）・運用 UI（embed.FS + html/template + HTMX + pico.css）を一手に引き受ける。

**パッケージ構成**:
- `cmd/axion/{main,server,client}.go` — サブコマンドディスパッチ（標準 flag）
- `internal/proto` — WS メッセージ型 + JSON Envelope + ProtoVersion
- `internal/server/{app, http, hub, syncengine, store, blobstore, web}` — サーバー側
- `internal/client/{app, conn, id, transfer, runner}` — クライアント側
- `internal/clientfs` — `Jail` ヘルパー（パストラバーサル防御）
- `internal/watcher` — 既存流用（debounce 200ms 化、Opener 注入）
- `pkg/logctx` — slog 共通フィールド付与

**主要設計判断**:
- 制御プレーン = WebSocket（多重化、ping/pong）/ データプレーン = HTTP（Range レジューム、ストリーム I/O）
- 真実の単一ソース = SQLite `file_state`、サーバー受信時刻 `server_mod_time` を LWW のタイムソースに統一
- セキュリティの基盤 = `clientfs.Jail` 一元化 + `forbidigo` で `os.Open*` 直接利用禁止
- `syncengine` は `Sender` interface 経由で hub に依存（依存逆転）
- 同時実行制御 = per-client semaphore (cap=4) + global semaphore (cap=64)

**スコープ外（v0.3 以降）**: 3 者以上同期、直接 P2P 転送、自動 TLS、マルチユーザー / RBAC、Prometheus メトリクス、Windows 公式サポート（v0.2 は best-effort）。

---

## v0.2.0 — Tabula Rasa

**ゴール**: v0.1 のコードを廃棄し、v0.2 の土台（サブコマンド・proto・logctx・clientfs・watcher 改修）を整える。

**完動品としての価値**: `axion server` と `axion client` のサブコマンドが認識され、それぞれが flag をパースしてバナーを出して終了する。proto パッケージで Envelope の round-trip テストが通る。

### Group 1 (並列実行可能)

- [x] Task 1.1: v0.1 コードの完全廃棄 — `internal/peer`, `internal/protocol`, `internal/syncer`, `internal/integration`, 旧 `cmd/*` を削除し、go.mod の不要依存を整理（`go build ./...` がエラーで落ちる状態でよい）
- [x] Task 1.2: `pkg/logctx/logctx.go` を新設 — `With(ctx, ...slog.Attr) context.Context` と `From(ctx) *slog.Logger` を実装し、`client_id`/`pair_id`/`rel_path`/`sha256[0:8]` の共通フィールド付与を提供
- [x] Task 1.3: `internal/proto` パッケージを新設 — `Envelope{Type, CorrelationID, Payload}`, **`RegisterRequest`/`RegisterResponse{ok, server_time, settings}`, `SubscribePair{pair_id, side, root_subpath}`/`SubscribePairAck`, `FileChangedEvent`, `FileSyncCommand{op: fetch|rename|delete, originator_client_id}`, `FileSyncAck{pair_id, side, rel, sha, status}`**, `ListDirRequest/Response`, `ListFilesRequest/Response`, `Ping/Pong`, `ProtoVersion="1"` を定義し、`Marshal/Unmarshal` を実装。全メッセージ型の round-trip テスト + unknown フィールド許容テストが green
  - **★ Critic C1 反映**: `SubscribePair`, `SubscribePairAck`, `RegisterResponse`, `FileSyncAck` を必ず含める

### Group 2 (Group 1 完了後)

- [x] Task 2.1: `cmd/axion/main.go` をサブコマンドディスパッチへ書き換え — `os.Args[1]` で `server`/`client` 振り分け、`flag.NewFlagSet` で各サブコマンドの flag を実装、`--help` ハンドリング、未知サブコマンドはエラー
- [x] Task 2.2: `internal/clientfs/jail.go` の新設 (TDD) — `Jail.Resolve(rel)` で `filepath.Clean` → `filepath.EvalSymlinks` → `filepath.Rel` 検証、`Open/Create/Rename/Stat/ReadDir` を提供、`ErrPathEscape`/`ErrUnsupportedFileType` を定義。`..`/絶対パス/symlink 経由の脱出全てが `ErrPathEscape` で弾かれるテスト + symlink/FIFO/socket/device で `ErrUnsupportedFileType` が返るテスト
- [x] Task 2.3: `internal/watcher` の改修 — debounce 100ms→200ms 化、package-level `shouldIgnore` を起動時注入式に変更、**`Opener interface { Open(rel string) (io.ReadCloser, error) }` および `Stater interface { Stat(rel string) (os.FileInfo, error) }` を導入し、watcher 内の全 `os.Open`/`os.Stat` を interface 経由に置換**
  - **★ Critic M1 反映**: forbidigo lint と矛盾しないようスコープを拡張
- [x] Task 2.4: `golangci-lint` 設定追加 — `forbidigo` で `os.Open*`/`os.ReadFile`/`os.WriteFile`/`os.Create`/**`os.Stat`** の直接利用禁止 (`internal/clientfs` のみ allow)。`golangci-lint run` が green

---

## v0.2.1 — Server Skeleton

**ゴール**: SQLite + migrations + WS Hub を立ち上げ、クライアントが登録・ping/pong できる最小サーバーを作る。

**完動品としての価値**: `axion server` 起動 → DB ファイル生成・migration 適用 → `axion client` が接続 → `clients` テーブルに行が入り、online/offline 状態が ping/pong で更新される。

### Group 1 (並列実行可能)

- [ ] Task 1.1: `internal/server/store` 実装 — `Store` interface 定義、`modernc.org/sqlite` 実装、`?_journal=WAL&_busy_timeout=5000` で開く、`server_mod_time` 単調性ガード（起動時 `MAX(server_mod_time)` 取得 + `time.Now() < max` で abort）。**正常系（max < now）と異常系（max >= now で abort）両方のテスト**
- [ ] Task 1.2: `internal/server/store/migrations` 実装 — `embed.FS` で migrations 同梱:
  - `0001_init.sql`: clients (**`etag INTEGER NOT NULL DEFAULT 1` 含む** ★ Critic M3 反映), sync_pairs, file_state, sync_runs, audit_log
  - `0002_settings.sql`: settings テーブル + ignore_list/blob_gc_age_seconds/max_file_size_bytes 初期値
  - `schema_version` テーブルベースで `BEGIN; ...; COMMIT;` 適用、partial 失敗で abort

### Group 2 (Group 1 完了後)

- [ ] Task 2.1: `internal/server/hub` 実装 — `Hub` で WS 接続を `ClientID→Conn` map で管理、`conn.go` で読み書きループ + 10s ping/30s pong-timeout、**`SetReadLimit(64*1024)` を WS 接続時に必ず設定** (★ 欠落補完)、`pending.go` で `correlation_id` ベースの request/response 相関
- [ ] Task 2.2: `internal/server/http/ws.go` + `middleware.go` — `/v1/ws` ハンドラで Bearer PSK 認証、proto_version 検証 (`Axion-Proto-Version: 1` HTTP ヘッダも併用)、`RegisterRequest` 受信 → `store.UpsertClient` → `Hub.Register` → **`audit_log.kind="client_register"` 記録 + `RegisterResponse{ok, server_time, settings}` を返送** (★ Critic M4 反映)。PSK 不一致 → `audit_log.kind="psk_auth_failed"`、proto_version 不一致 → `audit_log.kind="proto_version_mismatch"` のテスト
- [ ] Task 2.3: `internal/server/app.go` + `cmd/axion/server.go` 起動 orchestrator — `--bind`/`--data-dir`/`--admin-user`/`--admin-password`/`--psk-file` を受け、store 初期化 → **`store.LoadSettings()` で全 settings をメモリ上のアプリケーション設定に流し込み** (★ Critic M6 反映) → hub 起動 → http server 起動、`http.Server.Shutdown` の signal 連動

### Group 3 (Group 2 完了後)

- [ ] Task 3.1: `internal/client/id` + `internal/client/conn` 実装 — `~/.config/axion/client.id` の UUIDv4 生成・読み書き、サーバー WS クライアント (Bearer PSK, ping/pong, exponential backoff 1→60s ±20% jitter)。ID ファイル生成テスト、再接続 backoff の単体テスト (時刻 mock)
- [ ] Task 3.2: `internal/client/app.go` + `cmd/axion/client.go` 最小実装 — `--server`/`--root`/`--id-file`/`--psk-file` を受け、ID ロード → WS 接続 → RegisterRequest 送信 → **`RegisterResponse` 受信 → settings 反映** → ping/pong ループ。手動 E2E でサーバーに接続・登録できる、SIGTERM で graceful 終了

---

## v0.2.2 — Blob Plane

**ゴール**: HTTP blob 転送 (PUT/GET/HEAD + Range + .partial atomic rename) と、クライアント側 transfer モジュールを完成させる。

**完動品としての価値**: 任意の sha256 でファイルをサーバーへ PUT し、別クライアントから GET (Range レジューム含む) で取得できる。サーバー側でディスク上に正しく永続化される。

### Group 1 (並列実行可能)

- [ ] Task 1.1: `internal/server/blobstore` 実装 — `BlobStore` interface (Put/Get/Stat/Delete/GC)、`fs.go` で `<data-dir>/blobs/<sha[0:2]>/<sha>.partial` → SHA 検証 → atomic rename、Range 対応の `Get(offset, length)`。`t.TempDir()` で `.partial` 経由 atomic rename、Range レジューム、SHA 不一致で 422、ディスクフル擬似で 5xx のテスト
- [ ] Task 1.2: `internal/server/http/blobs.go` ハンドラ — **`HEAD /v1/blobs/{sha256}` (在庫確認、200/404)** (★ Critic M5 反映)、`PUT /v1/blobs/{sha256}` (1MB チャンクストリーム + `Content-Length` 検証 + `max_file_size_bytes` 制約 + **per-client quota 10GB 検査: 該当クライアントの blob 合計バイト数を集計し超過時 507 + `audit_log.kind="quota_exceeded"`** ★ Critic M7 反映)、`GET /v1/blobs/{sha256}` (`Content-Range` 対応)。100MB 送受信、ディスクフル時 5xx + `audit_log.kind="disk_full"`、size 超過で拒否、quota 超過で 507 のテスト

### Group 2 (Group 1 完了後)

- [ ] Task 2.1: `internal/client/transfer/upload.go` — Jail 経由でファイルを開き、`HEAD /v1/blobs/{sha}` 在庫確認 → なければ `PUT /v1/blobs/{sha}` ストリーム送信、あれば skip。大ファイル (100MB) アップロード、HEAD で 200 なら PUT skip、エラー時の再試行
- [ ] Task 2.2: `internal/client/transfer/download.go` — `GET /v1/blobs/{sha}` を Range 付きで取得、Jail 経由で `<rel>.partial` 書き込み → SHA 検証 → atomic rename。切断後 Range レジュームで完成、SHA 不一致で .partial 残置 + エラー、Jail で root 外パス拒否のテスト

---

## v0.2.3 — One-Way Sync MVP

**ゴール**: watcher → FileChangedEvent → 片方向 strict mirror → FileSyncCommand → download の最小同期パイプラインを通す。

**完動品としての価値**: 同期ペア (A→B, mirror) を Web UI 不在のため SQL で手動投入した状態で、クライアント A の `/dirA` 配下のファイル変更が WS と HTTP を経由して B の `/dirB` に伝播し、B 側の変更は次回 A 側更新時に上書きされる。bidirectional ペアは v0.2.3 では「無視 + warn ログ」とする（v0.2.4 で実装）。★ Critic m4 反映

### Group 1 (順序実行: 1.1 → 1.2 → 1.3)

- [ ] Task 1.1: `internal/server/syncengine` 骨格 — `Engine` 構造体、`Sender interface { Send(ctx, clientID, msg) error }` 定義 (hub に依存しない)、`HandleFileChanged` のエントリポイント、`mirror.go` で strict mirror 判定、**bidirectional 受信時は warn ログ + skip**、**同期完了/失敗で `sync_runs` に行を書き込む** (★ Critic M8 反映)。フェイク Sender でテスト
- [ ] Task 1.2: `internal/server/syncengine/scheduler.go` — `golang.org/x/sync/semaphore` で per-client cap=4 + global cap=64、overflow channel-based FIFO キュー、`(client_id, pair_id, rel)` での dedup、context cancel で release。上限テスト・dedup テスト・cancel テスト
- [ ] Task 1.3: **サーバー側 SubscribePair 配信** — クライアント接続時 (`store.PairsForClient(client_id)` で参加ペア取得 → `SubscribePair{pair_id, side, root_subpath}` を該当クライアントへ送信)、ペア作成/更新/有効化/無効化時にも該当クライアントへ差分配信。`SubscribePairAck` を受けて配信完了とする
  - **★ Critic C3 反映**: SubscribePair 配信タスクが従来欠落していた

### Group 2 (Group 1 完了後)

- [ ] Task 2.1: `internal/client/runner/watcher.go` — `internal/watcher` を Jail と統合、`shouldIgnore` を settings から注入、200ms デバウンス後 `FileChangedEvent` を WS 送信。symlink/FIFO/socket/device は audit 用に skip 通知（`audit_log.kind="unsupported_file_skipped"`）
- [ ] Task 2.2: **`internal/client/runner/subscription.go` — SubscribePair レジストリ** — 受信した SubscribePair を `map[pairID]Subscription{side, rootSubpath}` に保存、watcher イベントから rel_path を見て該当する `(pair_id, side)` を解決して `FileChangedEvent` を生成（複数ペアが root 配下に共存する場合に対応）
  - **★ Critic C2 反映**: pair_id/side マッピングロジックが従来欠落していた
- [ ] Task 2.3: `internal/client/runner/suppress.go` — originator suppress (TTL=2s, 10s GC)、`Add(rel, ttl)` / `Hit(rel) bool` 実装、**download 完了後に sha 一致 watcher 検知でも追加 suppress** (architect.md:308 補完)。並行 Add/Hit テスト、TTL 経過で expire、GC 動作
- [ ] Task 2.4: `internal/client/runner/app.go` — runner オーケストレータ、SubscribePair 受信 → subscription レジストリ更新 → watcher → upload → FileChangedEvent → 受信した FileSyncCommand → download → suppress.Add → FileSyncAck の順序

### Group 3 (Group 2 完了後)

- [ ] Task 3.1: 結合テスト I1 (片方向版) — `httptest.NewServer` でサーバー起動 + クライアント A,B を同プロセス goroutine、SQL で mirror ペアを投入、`/dirA/foo.txt` 作成 → 500ms 以内に `/dirB/foo.txt` が同 sha で出現。build tag `integration` で go test green

---

## v0.2.4 — Bidirectional + Conflict

**ゴール**: 双方向 LWW、コンフリクトファイル退避、削除 vs 編集の競合解決を実装する。

**完動品としての価値**: 双方向ペアで両側同時変更が起きた場合、サーバー受信時刻 LWW で勝者決定し、負け側は `<name>.conflict-<short>-<unix_ns>` に退避される。削除 vs 編集では編集側が勝つ。

### Group 1 (並列実行可能)

- [ ] Task 1.1: `internal/server/syncengine/lww.go` — 双方向 LWW 比較 (server_mod_time の単調性に依拠)、`UPSERT ... WHERE server_mod_time > excluded` の DB ガードと協調。A 新 / B 新 / 同時刻同 sha / 同時刻異 sha / delete vs write の全マトリクステスト
- [ ] Task 1.2: `internal/server/syncengine/conflict.go` — 負け側を `<name>.conflict-<client_id_short>-<unix_ns>` 形式へ退避する `FileSyncCommand{op=rename}` 発行、勝ち側を `op=fetch` で配布、**`audit_log.kind="conflict_detected"` + `conflict_renamed"` 記録**
- [ ] Task 1.3: `internal/server/syncengine` の delete vs edit 処理 — 片側 op=delete・片側 op=write を検出し write 側を勝者、delete 側に `op=fetch` 再生成、**`audit_log.kind="delete_vs_edit"` 記録**、`sync_pairs` の警告フラグ立て

### Group 2 (Group 1 完了後)

- [ ] Task 2.1: クライアント側 rename 処理 — `FileSyncCommand{op=rename}` を Jail 経由で実行、suppress と協調。rename 中 watcher が発火しないテスト
- [ ] Task 2.2: 結合テスト I3 — 同時変更で `.conflict-` ファイルが残ることを E2E 確認。build tag `integration` で green

---

## v0.2.5 — Web UI Core

**ゴール**: embed テンプレ + Basic 認証 + CSRF + クライアント一覧 + ペア CRUD + settings 編集を実装する。

**完動品としての価値**: 管理者がブラウザで `/` `/pairs` `/settings` を開き、クライアント一覧を見ながら同期ペアを CRUD でき、ignore_list 等の設定を編集できる。

### Group 1 (並列実行可能)

- [ ] Task 1.1: `internal/server/web/assets/` — `pico.css`、`htmx.min.js`、共通レイアウト・base.tmpl を配置し `embed.FS` で同梱。`/static/*` で配信される
- [ ] Task 1.2: `internal/server/http/middleware.go` 拡張 — HTTP Basic 認証 (`--admin-user`/`--admin-password`)、CSRF double-submit cookie (セッション中固定)、slog access log。認証なしで 401、CSRF 不一致で 403、両者 OK で通過のテスト

### Group 2 (Group 1 完了後)

- [ ] Task 2.1: `/` クライアント一覧ページ — display_name (未設定時は **hostname フォールバック** ★ 欠落補完) / hostname / root_path / status / last_seen を表示、display_name 編集 UI (HTMX 差分 swap, **`If-Match: <etag>` 楽観ロック、サーバー側で 412 ハンドラ実装** ★ Critic M3/欠落補完)。一覧表示 + 編集 + etag 競合で 412 のテスト
- [ ] Task 2.2: `/pairs` ペア一覧 + CRUD ページ — 一覧、新規作成フォーム (クライアント A・パス A・クライアント B・パス B・direction・name・enabled)、編集、削除、HTMX 差分 swap、楽観ロック、**ペア CRUD 時に `audit_log.kind="pair_create"|"pair_update"|"pair_delete"` 記録 + 単体テストで確認** (★ Critic M4 反映)、**enabled 0→1 切り替え時に SubscribePair 再配信 + 該当クライアントに差分スキャン要求** (★ 欠落補完: spec.md:103)
- [ ] Task 2.3: **`/settings` ページ — `settings` テーブルの key/value 編集 (HTMX 差分 swap、楽観ロック)**、ignore_list はカンマ区切り or JSON 配列で編集
  - **★ Critic M2 反映**: settings Web UI 編集が従来欠落していた

---

## v0.2.6 — Web UI Live

**ゴール**: ListDir ブラウザ、`/runs` 履歴、hx-ws による online/offline 自動反映を実装する。

**完動品としての価値**: ペア作成時にディレクトリツリーから対象パスを選択でき、実行履歴が `/runs` で確認でき、クライアント状態変化が画面リロードなしで反映される。

### Group 1 (並列実行可能)

- [ ] Task 1.1: `internal/server/http/api.go` の `GET /v1/clients/{id}/listdir?path=...` — Hub 経由で対象クライアントへ `ListDirRequest` 送信、`pending.go` の correlation_id で応答待ち、タイムアウト 5s
- [ ] Task 1.2: `internal/client/runner/listdir.go` — `ListDirRequest` を Jail 経由で実行、`is_dir` フラグ付き entries を返す、root 外要求は 400 + `audit_log.kind="path_escape_blocked"`

### Group 2 (Group 1 完了後)

- [ ] Task 2.1: `/clients/{id}/browse?path=/` ツリーブラウザ — ListDir 経由でディレクトリツリーを HTMX で展開、ファイルは表示のみ操作不可、display_name + 絶対パス併記
- [ ] Task 2.2: `/runs` 実行履歴ページ — `sync_runs` 一覧 (ペア名 / 開始終了時刻 / 状態 / バイト数 / ファイル数 / コンフリクト数 / エラー)、ページング、status フィルタリング
- [ ] Task 2.3: `internal/server/web/htmx.go` `hx-ws` ブロードキャスタ — **専用エンドポイント `/admin/ws` (Basic 認証、制御プレーン `/v1/ws` とは別)** ★ Critic m5 反映、Hub の online/offline 変化を pub/sub し Web UI クライアントへ HTML フラグメントを push。状態変化が画面に反映される手動確認 + 結合テストでフラグメント送信を確認

---

## v0.2.7 — Hardening

**ゴール**: 再接続時差分スキャン、graceful shutdown 完成、blob GC、audit_log カバレッジ verification、結合テスト I2/I4/I5/I6、case-insensitive FS 警告、cross-platform smoke test。リリース可能品質に到達する。

**完動品としての価値**: 切断・再接続・障害復旧・長時間停止後の同期再開・大ファイル転送の Range レジュームが安定動作し、運用に耐える品質となる。

### Group 1 (並列実行可能)

- [ ] Task 1.1: `internal/client/runner/snapshot.go` 差分スキャン — 再接続後 `ListFilesRequest` を受信し、Jail で root_subpath を walk → `(rel, sha, mtime, size, is_dir)` で応答、空ディレクトリも含む。100,000 ファイルで完走、深度 32 超過で警告ログ、シンボリックリンク skip + audit
- [ ] Task 1.2: サーバー側 `syncengine` の差分処理 — `ListFilesResponse` を `file_state` と diff し、不在/古い側へ `FileSyncCommand` を LWW/mirror に従って発行。5 ファイル変更後再接続でキャッチアップ完了の単体テスト
- [ ] Task 1.3: `internal/server/blobstore` GC — `settings.blob_gc_age_seconds` (デフォルト 7 日) 経過 + `file_state.sha256` 未参照の blob を 1 時間 ticker で削除。古い未参照 blob のみ削除されるテスト、参照中保護のテスト
- [ ] Task 1.4: **`internal/clientfs.Jail` の `case_insensitive_fs` 起動時チェック** — root 配下に `.axion-case-test` と `.AXION-CASE-TEST` を作って同 inode を指すかチェック → 同じなら warn ログ + `audit_log.kind="case_insensitive_fs"`、Web UI で警告表示
  - **★ 欠落補完**: architect.md:443

### Group 2 (Group 1 完了後)

- [ ] Task 2.1: **audit_log カバレッジ verification** — 各 milestone で実装済みの kind (`client_register`, `pair_create/update/delete`, `conflict_detected`, `conflict_renamed`, `delete_vs_edit`, `unsupported_file_skipped`, `path_escape_blocked`, `proto_version_mismatch`, `psk_auth_failed`, `disk_full`, `quota_exceeded`, `case_insensitive_fs`) の発火を網羅的に E2E 確認、Web UI で表示確認
  - **★ Critic M4 反映**: 各 milestone で実装済みのため verification only
- [ ] Task 2.2: graceful shutdown 完成 — クライアント SIGTERM/SIGINT で `last_seen` 即時更新リクエスト送信 + Hub.Unregister、サーバー SIGTERM で in-flight 同期 drain (timeout 30s)。シャットダウン時に進行中転送が完了するテスト、強制 kill で .partial 残置 → 再起動で Range レジュームのテスト
- [ ] Task 2.3: 結合テスト I2/I4/I5/I6 追加 — I2 (5 ファイル変更後再接続でキャッチアップ)、I4 (PSK 不一致 + audit)、I5 (パストラバーサル → 400 + audit)、I6 (大ファイル切断 → Range 完成)

### Group 3 (Group 2 完了後)

- [ ] Task 3.1: ドキュメント整備 — README に server/client 起動手順、`docs/PROTOCOL.md` で proto_version 互換ポリシー (メジャー一致必須、マイナー後方互換、unknown フィールド無視) を明記、WS プロキシ越しの keepalive ping/タイムアウト値推奨設定を documentation化
- [ ] Task 3.2: リリースチェック — `go build` 単一バイナリ確認 (CGO_ENABLED=0)、macOS/Linux 両方で smoke test、Windows best-effort smoke test (case-insensitive FS 警告動作確認)。全プラットフォームでサーバー起動 + 1 クライアント接続 + 1 ファイル同期

---

## 留保事項 (Critic レビュー結果より)

Critic 判定: **REVISE**（局所修正で ACCEPT 相当に到達可能）。Critical 3件 + Major 8件は本ロードマップに直接反映済み。以下は autopilot 実行時に注意すべき残課題:

- **m1 (parallelism)**: v0.2.3 Group 1 は同 syncengine パッケージ依存のため、1.1 → 1.2 → 1.3 の順次実行（並列不可）に変更済み
- **m2 (forbidigo scope)**: `os.Stat` を禁止リストに含めた。さらに `filepath.Walk` も含めるかは Open Question
- **m3 (monotonic guard)**: 正常系・異常系両方のテストを Task 1.1 (v0.2.1) に明示
- **未確認の暗黙仮定**: `RegisterResponse.settings` の中身（どの settings キーをクライアントに配布するか）はサーバー側 Task 2.2 (v0.2.1) 実装時に決定する

## Open Questions

- [ ] `forbidigo` 禁止リストに `filepath.Walk` を含めるか — `Jail` 経由 walk のヘルパーを提供する場合に限り禁止可能
- [ ] v0.2.6 の `hx-ws` ブロードキャスト粒度 — クライアント単位の状態変化を全 Web UI セッションへ push するが、将来マルチユーザー化時の pub/sub 設計を v0.2 段階でどこまで先取りするか
- [ ] v0.2.7 の Windows best-effort 範囲 — case-insensitive FS 警告とパス区切り正規化以外の Windows 固有問題 (長パス、予約名 CON/PRN、ACL) を v0.2 で扱うか v0.3 へ送るか
- [ ] 結合テスト I7 (ディスクフル) を v0.2.7 で追加するか v0.3 へ送るか — CI 実行環境で擬似ディスクフルを再現する手段が要検討
- [ ] `RegisterResponse.settings` に含めるキーの確定（ignore_list は確定、それ以外）
