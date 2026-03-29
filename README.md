# test-openai-appssdk

`badlogic/pi-mono` の `openai-codex.ts` を参考にした、OpenAI OAuth + PKCE の Go サンプルです。

やっていること:

- PKCE `S256` で authorization URL を生成
- 可能ならブラウザを自動起動
- `http://127.0.0.1:1455/auth/callback` でローカル callback を受ける
- ブラウザ起動や callback が使えない場合は、authorization code または redirect URL の手入力にフォールバック
- authorization code を access token / refresh token に交換
- refresh token で access token を更新
- 取得した token を `auth.json` に保存
- access token の JWT から `chatgpt_account_id` を読む

## 前提

このサンプルは OpenAI OAuth client として動きます。Apps SDK の「自前認可サーバー」サンプルではありません。

`pi-mono` と同様の流れです。

1. ローカル callback サーバーを立てる
2. OpenAI の authorize URL をブラウザで開く
3. callback で code を受ける
4. token endpoint で code を token に交換する
5. `auth.json` に保存する

## 環境変数

最低限これを設定してください。

```bash
export OPENAI_OAUTH_CLIENT_ID=your_client_id
```

任意:

```bash
export OPENAI_OAUTH_AUTHORIZE_URL=https://auth.openai.com/oauth/authorize
export OPENAI_OAUTH_TOKEN_URL=https://auth.openai.com/oauth/token
export OPENAI_OAUTH_REDIRECT_URL=http://localhost:1455/auth/callback
export OPENAI_OAUTH_SCOPE="openid profile email offline_access"
export OPENAI_OAUTH_ORIGINATOR=codex_cli
export OPENAI_OAUTH_AUTH_FILE="$HOME/.config/test-openai-appssdk/auth.json"
```

## 使い方

ログイン:

```bash
go run . login
```

`login` はまずブラウザ起動を試みます。失敗した場合や callback を受けられない場合は、端末に `authorization code` または callback 後の URL 全体を貼り付ければ続行できます。

保存済み refresh token で更新:

```bash
go run . refresh
```

保存済み access token を表示:

```bash
go run . token
```

## 保存先

既定では `os.UserConfigDir()/test-openai-appssdk/auth.json` に保存します。`OPENAI_OAUTH_AUTH_FILE` で変更できます。

## CLIENT_ID 取得補助

`openai/codex` の GitHub code search から `CLIENT_ID` を引く補助コマンドを Go で入れています。

GitHub token の解決順は以下です。

1. `GITHUB_TOKEN`
2. `GH_TOKEN`
3. `gh auth token`

環境変数を使う場合:

```bash
export GITHUB_TOKEN=your_github_token
```

または:

```bash
export GH_TOKEN=your_github_token
```

```bash
go run ./hack/get-openai-client-id
```

これは GitHub Search API と contents API を直接読んで `CLIENT_ID` を抽出します。`GITHUB_TOKEN` / `GH_TOKEN` が無い場合は `gh auth token` にフォールバックします。

## TDD

以下をテストで固定しています。

- authorize URL に PKCE と OpenAI 用 query parameter が入ること
- callback の `state` 検証
- ブラウザ起動失敗時の手入力フォールバック
- authorization code の token exchange
- refresh token での更新
- `auth.json` の保存と読込
- JWT からの `chatgpt_account_id` 抽出
