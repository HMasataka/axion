# Axion テスト計画書

## 1. 単体テスト (Unit Tests)

### 1.1 Config パッケージ

| テスト名                   | 内容                               |
| -------------------------- | ---------------------------------- |
| TestDefaultConfig          | デフォルト設定値の検証             |
| TestConfigLoad             | JSONファイルからの設定読み込み     |
| TestConfigLoad_InvalidJSON | 不正なJSONファイルの読み込みエラー |
| TestConfigLoad_NotExist    | 存在しないファイルのエラー         |
| TestConfigSave             | 設定ファイルの保存                 |
| TestConfigSave_CreateDir   | 親ディレクトリの自動作成           |
| TestGetConfigPath          | 設定ファイルパスの取得             |
| TestLoadOrCreate_Create    | 設定ファイルがない場合の新規作成   |
| TestLoadOrCreate_Load      | 既存設定ファイルの読み込み         |

### 1.2 Protocol パッケージ

| テスト名                           | 内容                           |
| ---------------------------------- | ------------------------------ |
| TestEncode                         | メッセージのエンコード         |
| TestDecode                         | メッセージのデコード           |
| TestEncodeDecode_RoundTrip         | エンコード→デコードの往復      |
| TestDecode_InvalidLength           | 不正なLength値のエラー         |
| TestDecode_TruncatedPayload        | 途中で切れたペイロードのエラー |
| TestDecode_InvalidJSON             | 不正なJSONのエラー             |
| TestNewFileChangeMessage           | FileChangeメッセージ生成       |
| TestNewFileRequestMessage          | FileRequestメッセージ生成      |
| TestNewFileDataMessage             | FileDataメッセージ生成         |
| TestNewFileDeleteMessage           | FileDeleteメッセージ生成       |
| TestNewSyncRequestMessage          | SyncRequestメッセージ生成      |
| TestNewSyncResponseMessage         | SyncResponseメッセージ生成     |
| TestParseFileChangePayload         | FileChangeペイロードのパース   |
| TestParseFileChangePayload_Invalid | 不正なペイロードのエラー       |
| TestParseFileRequestPayload        | FileRequestペイロードのパース  |
| TestParseFileDataPayload           | FileDataペイロードのパース     |
| TestParseFileDataPayload_LargeData | 大容量データのパース           |
| TestParseFileDeletePayload         | FileDeleteペイロードのパース   |
| TestParseSyncRequestPayload        | SyncRequestペイロードのパース  |
| TestParseSyncResponsePayload       | SyncResponseペイロードのパース |

### 1.3 Watcher パッケージ

| テスト名                              | 内容                                        |
| ------------------------------------- | ------------------------------------------- |
| TestWatcherNew                        | Watcher生成                                 |
| TestWatcherNew_InvalidPath            | 存在しないパスのエラー                      |
| TestWatcherStart                      | 監視開始                                    |
| TestWatcherStop                       | 監視停止                                    |
| TestWatcherEvents_Create              | ファイル作成イベント検知                    |
| TestWatcherEvents_Write               | ファイル更新イベント検知                    |
| TestWatcherEvents_Remove              | ファイル削除イベント検知                    |
| TestWatcherEvents_Rename              | ファイルリネームイベント検知                |
| TestWatcherEvents_DirCreate           | ディレクトリ作成イベント検知                |
| TestWatcherEvents_Debounce            | デバウンス処理（100ms内の連続イベント集約） |
| TestWatcherShouldIgnore_Git           | `.git`ディレクトリの除外                    |
| TestWatcherShouldIgnore_DSStore       | `.DS_Store`ファイルの除外                   |
| TestWatcherShouldIgnore_ThumbsDb      | `Thumbs.db`ファイルの除外                   |
| TestWatcherShouldIgnore_TmpFiles      | `*.tmp`ファイルの除外                       |
| TestWatcherShouldIgnore_SwpFiles      | `*.swp`ファイルの除外                       |
| TestWatcherShouldIgnore_BackupFiles   | `*~`ファイルの除外                          |
| TestWatcherShouldIgnore_CustomPattern | カスタムパターンの除外                      |
| TestWatcherRecursive                  | サブディレクトリの再帰的監視                |
| TestWatcherCalculateHash              | SHA-256ハッシュ計算                         |
| TestWatcherCalculateHash_EmptyFile    | 空ファイルのハッシュ計算                    |
| TestWatcherGetFileHash                | ファイルハッシュの取得                      |
| TestWatcherSetFileHash                | ファイルハッシュの設定                      |

### 1.4 Peer パッケージ

| テスト名                   | 内容                       |
| -------------------------- | -------------------------- |
| TestNewClient              | クライアント生成           |
| TestNewFromConn            | 接続からのPeer生成         |
| TestPeerConnect            | 接続確立                   |
| TestPeerConnect_Timeout    | 接続タイムアウト           |
| TestPeerConnect_Refused    | 接続拒否エラー             |
| TestPeerClose              | 接続クローズ               |
| TestPeerSend               | メッセージ送信             |
| TestPeerSend_NotConnected  | 未接続時のエラー           |
| TestPeerSend_ChannelFull   | 送信チャネル満杯時のエラー |
| TestPeerIsConnected        | 接続状態の確認             |
| TestPeerStartWithReconnect | 自動再接続                 |
| TestPeerReconnect_Interval | 再接続間隔（5秒）          |
| TestServerNew              | サーバー生成               |
| TestServerStart            | サーバー起動               |
| TestServerStart_PortInUse  | ポート使用中エラー         |
| TestServerStop             | サーバー停止               |
| TestServerAcceptConnection | 接続受け入れ               |
| TestServerBroadcast        | 全ピアへのブロードキャスト |
| TestServerGetPeers         | 接続中ピア一覧取得         |
| TestServerRemovePeer       | ピア削除                   |
| TestServerSetPeerHandler   | ピアハンドラ設定           |

### 1.5 Syncer パッケージ

| テスト名                                | 内容                             |
| --------------------------------------- | -------------------------------- |
| TestSyncerNew                           | Syncer生成                       |
| TestSyncerNew_InvalidPath               | 不正なパスのエラー               |
| TestSyncerStart                         | 同期開始                         |
| TestSyncerStop                          | 同期停止                         |
| TestSyncerHandleLocalChange_Create      | ローカルファイル作成の処理       |
| TestSyncerHandleLocalChange_Write       | ローカルファイル更新の処理       |
| TestSyncerHandleLocalChange_Remove      | ローカルファイル削除の処理       |
| TestSyncerHandleLocalChange_Rename      | ローカルファイルリネームの処理   |
| TestSyncerHandleFileChange              | FileChangeメッセージの処理       |
| TestSyncerHandleFileChange_SameHash     | 同一ハッシュ時のスキップ         |
| TestSyncerHandleFileChange_LocalNewer   | ローカルが新しい場合のスキップ   |
| TestSyncerHandleFileChange_RemoteNewer  | リモートが新しい場合のリクエスト |
| TestSyncerHandleFileChange_Dir          | ディレクトリ作成の処理           |
| TestSyncerHandleFileRequest             | FileRequestメッセージの処理      |
| TestSyncerHandleFileRequest_NotExist    | 存在しないファイルのエラー       |
| TestSyncerHandleFileData                | FileDataメッセージの処理         |
| TestSyncerHandleFileData_CreateDir      | 親ディレクトリの自動作成         |
| TestSyncerHandleFileDelete              | FileDeleteメッセージの処理       |
| TestSyncerHandleFileDelete_Dir          | ディレクトリ削除の処理           |
| TestSyncerHandleSyncRequest             | SyncRequestメッセージの処理      |
| TestSyncerHandleSyncResponse            | SyncResponseメッセージの処理     |
| TestSyncerInitialSync                   | 初期同期処理                     |
| TestSyncerScanLocalFiles                | ローカルファイルスキャン         |
| TestSyncerScanLocalFiles_IgnorePatterns | 除外パターン適用                 |
| TestSyncerCalculateFileHash             | ファイルハッシュ計算             |
| TestSyncerBroadcast                     | メッセージブロードキャスト       |
| TestSyncerGetStatus                     | ステータス取得                   |
| TestSyncerGetStatusJSON                 | JSON形式ステータス取得           |
| TestSyncerSyncingFilesFlag              | 同期中フラグによるループ防止     |

## 2. 結合テスト (Integration Tests)

### 2.1 P2P通信テスト

| テスト名                           | 内容                               |
| ---------------------------------- | ---------------------------------- |
| TestP2P_ClientServerConnection     | クライアント・サーバー間の接続確立 |
| TestP2P_MessageExchange            | メッセージの送受信                 |
| TestP2P_BidirectionalCommunication | 双方向通信                         |
| TestP2P_MultipleClients            | 複数クライアント接続               |
| TestP2P_Reconnection               | 切断後の自動再接続                 |
| TestP2P_LargeMessage               | 大容量メッセージの送受信           |

### 2.2 ファイル同期テスト

| テスト名                 | 内容                         |
| ------------------------ | ---------------------------- |
| TestSync_CreateFile      | ファイル作成の同期           |
| TestSync_UpdateFile      | ファイル更新の同期           |
| TestSync_DeleteFile      | ファイル削除の同期           |
| TestSync_RenameFile      | ファイルリネームの同期       |
| TestSync_CreateDirectory | ディレクトリ作成の同期       |
| TestSync_DeleteDirectory | ディレクトリ削除の同期       |
| TestSync_NestedDirectory | ネストしたディレクトリの同期 |
| TestSync_BinaryFile      | バイナリファイルの同期       |
| TestSync_LargeFile       | 大容量ファイルの同期         |
| TestSync_ManyFiles       | 多数ファイルの同期           |
| TestSync_RapidChanges    | 連続した変更の同期           |
| TestSync_InitialSync     | 初期同期（起動時）           |
| TestSync_Bidirectional   | 双方向同期                   |

### 2.3 競合解決テスト

| テスト名                   | 内容                         |
| -------------------------- | ---------------------------- |
| TestConflict_RemoteNewer   | リモートが新しい場合の上書き |
| TestConflict_LocalNewer    | ローカルが新しい場合の保持   |
| TestConflict_SameTimestamp | 同一タイムスタンプ時の動作   |
| TestConflict_SameContent   | 同一内容時のスキップ         |

## 3. クロスプラットフォームテスト

### 3.1 パス処理テスト

| テスト名              | 内容                                   |
| --------------------- | -------------------------------------- |
| TestPath_ToSlash      | バックスラッシュからスラッシュへの変換 |
| TestPath_FromSlash    | スラッシュからバックスラッシュへの変換 |
| TestPath_DeepNesting  | 深いネストのパス処理                   |
| TestPath_SpecialChars | 特殊文字を含むパス                     |
| TestPath_Unicode      | Unicode文字を含むパス                  |
| TestPath_Spaces       | スペースを含むパス                     |

### 3.2 ファイル権限テスト

| テスト名                  | 内容                             |
| ------------------------- | -------------------------------- |
| TestPermission_FileCreate | ファイル作成時の権限（0644）     |
| TestPermission_DirCreate  | ディレクトリ作成時の権限（0755） |

## 4. エラーハンドリングテスト

### 4.1 接続エラーテスト

| テスト名                           | 内容                             |
| ---------------------------------- | -------------------------------- |
| TestError_ConnectionRefused        | 接続拒否時の動作                 |
| TestError_ConnectionLost           | 接続断時の動作                   |
| TestError_NetworkTimeout           | ネットワークタイムアウト時の動作 |
| TestError_ReconnectAfterDisconnect | 切断後の再接続                   |

### 4.2 ファイルエラーテスト

| テスト名                      | 内容                   |
| ----------------------------- | ---------------------- |
| TestError_FileReadPermission  | 読み取り権限なしエラー |
| TestError_FileWritePermission | 書き込み権限なしエラー |
| TestError_DiskFull            | ディスク容量不足エラー |
| TestError_FileLocked          | ファイルロック中エラー |
| TestError_InvalidPath         | 不正なパスエラー       |

## 5. パフォーマンステスト

| テスト名                     | 内容                         |
| ---------------------------- | ---------------------------- |
| BenchmarkHash_SmallFile      | 小容量ファイルのハッシュ計算 |
| BenchmarkHash_LargeFile      | 大容量ファイルのハッシュ計算 |
| BenchmarkEncode              | メッセージエンコード         |
| BenchmarkDecode              | メッセージデコード           |
| BenchmarkScanFiles           | ファイルスキャン             |
| BenchmarkSync_ManySmallFiles | 多数の小容量ファイル同期     |
| BenchmarkSync_FewLargeFiles  | 少数の大容量ファイル同期     |

## 6. テスト実行方法

### 6.1 全テスト実行

```bash
go test ./...
```

### 6.2 パッケージ別実行

```bash
go test ./internal/config
go test ./internal/protocol
go test ./internal/watcher
go test ./internal/peer
go test ./internal/syncer
```

### 6.3 カバレッジ取得

```bash
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### 6.4 ベンチマーク実行

```bash
go test -bench=. ./...
```

### 6.5 レースコンディション検出

```bash
go test -race ./...
```

## 7. テスト環境

### 7.1 必要条件

- Go 1.21以上
- 一時ディレクトリへの読み書き権限
- ローカルホストでのTCP通信許可

### 7.2 テスト用ポート

- デフォルト: 18765（テスト用にオフセット）
- 複数テスト並行実行時は動的ポート割り当て

## 8. カバレッジ目標

| パッケージ | 目標カバレッジ |
| ---------- | -------------- |
| config     | 90%以上        |
| protocol   | 95%以上        |
| watcher    | 80%以上        |
| peer       | 80%以上        |
| syncer     | 75%以上        |
| 全体       | 80%以上        |
