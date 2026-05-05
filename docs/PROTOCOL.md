# Axion Protocol v1

## Versioning Policy

`ProtoVersion` 文字列は SemVer ライク (例: "1", "1.0", "1.1")。

- メジャー一致必須: "1" と "2" は非互換。サーバーは不一致を拒否します
- マイナーは後方互換: "1.1" サーバーは "1.0" クライアントを受け入れます
- 未知フィールドは無視します (前方互換)

## WebSocket Control Plane

- Endpoint: `/v1/ws`
- Auth: `Authorization: Bearer <psk>`
- 最大メッセージサイズ: 64KB
- Ping/Pong: 10 秒間隔、30 秒応答なしで offline 遷移
- Envelope: `{"type": "...", "correlation_id": "...", "payload": {...}}` (JSON)

### Messages

| type | direction | description |
| --- | --- | --- |
| `register_request` | C -> S | クライアント登録 |
| `register_response` | S -> C | 登録結果 + settings |
| `subscribe_pair` | S -> C | ペア subscribe (direction="" で unsubscribe) |
| `subscribe_pair_ack` | C -> S | 確認 |
| `file_changed_event` | C -> S | ファイル変更通知 |
| `file_sync_command` | S -> C | 同期指示 (fetch/rename/delete) |
| `file_sync_ack` | C -> S | 完了通知 |
| `list_dir_request` | S -> C | ディレクトリ列挙要求 |
| `list_dir_response` | C -> S | 結果 |
| `list_files_request` | S -> C | スナップショット要求 |
| `list_files_response` | C -> S | スナップショット |
| `ping` / `pong` | bi-dir | keepalive |

### register\_request (C -> S)

```json
{
  "type": "register_request",
  "payload": {
    "client_id": "<uuid>",
    "hostname": "my-machine",
    "root_path": "/home/user/sync",
    "version": "0.2.7",
    "proto_version": "1"
  }
}
```

### register\_response (S -> C)

```json
{
  "type": "register_response",
  "payload": {
    "ok": true,
    "server_time": 1700000000000000000,
    "settings": {}
  }
}
```

登録失敗時は `ok: false` と `reason` フィールドが返ります。

### file\_changed\_event (C -> S)

```json
{
  "type": "file_changed_event",
  "payload": {
    "pair_id": "<pair-uuid>",
    "side": "a",
    "rel_path": "docs/note.md",
    "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    "size": 1024,
    "mod_time": 1700000000000000000,
    "op": "write"
  }
}
```

削除時は `op: "delete"` とし、`sha256` / `size` は省略します。

### file\_sync\_command (S -> C)

```json
{
  "type": "file_sync_command",
  "payload": {
    "pair_id": "<pair-uuid>",
    "side": "b",
    "rel_path": "docs/note.md",
    "sha256": "e3b0c44...",
    "op": "fetch",
    "originator_client_id": "<client-uuid>"
  }
}
```

`op` は `"fetch"` / `"rename"` / `"delete"` のいずれかです。`rename` 時は `new_rel_path` が付きます。

## HTTP Data Plane

- Endpoint: `/v1/blobs/{sha256}`
- Auth: `Authorization: Bearer <psk>` および `X-Axion-Client-ID: <uuid>`

| Method | 説明 |
| --- | --- |
| HEAD | 在庫確認。200 (存在) / 404 (不在) |
| PUT | アップロード。`Content-Length` 必須。`Content-Range` 付きでレジューム可能 |
| GET | ダウンロード。`Range` ヘッダでパーシャル取得可能 |

### PUT (通常アップロード)

```text
PUT /v1/blobs/e3b0c44... HTTP/1.1
Authorization: Bearer <psk>
X-Axion-Client-ID: <uuid>
Content-Length: 1024
Content-Type: application/octet-stream

<binary data>
```

### PUT (レジューム)

```text
PUT /v1/blobs/e3b0c44... HTTP/1.1
Authorization: Bearer <psk>
X-Axion-Client-ID: <uuid>
Content-Length: 524288
Content-Range: bytes 524288-1048575/1048576

<binary data>
```

### GET (パーシャル)

```text
GET /v1/blobs/e3b0c44... HTTP/1.1
Authorization: Bearer <psk>
X-Axion-Client-ID: <uuid>
Range: bytes=0-1023
```

## Reverse Proxy 推奨設定

WebSocket を nginx / caddy 越しに使う場合の推奨設定です。

### nginx

```nginx
location /v1/ws {
    proxy_pass http://axion-backend:8765;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 600s;
    proxy_send_timeout 600s;
}

location / {
    proxy_pass http://axion-backend:8765;
}
```

`proxy_read_timeout` は ping 間隔 (10s) より十分大きな値を設定してください。

### Caddy

```text
reverse_proxy axion-backend:8765 {
    transport http {
        keepalive 600s
    }
}
```
