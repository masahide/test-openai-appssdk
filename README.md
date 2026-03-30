# codex-oauth-pkce

`badlogic/pi-mono` の `openai-codex.ts` を参考にした、`codex-oauth-pkce` 向けの OpenAI OAuth + PKCE フロー実装です。

## 警告

この実装は実験用です。OpenAI の公開ドキュメントでは、ChatGPT/Codex 系 OAuth を外部ツールから利用する方法や、外部ツール向け `client_id` の扱いが十分に明文化されていません。

そのため、このリポジトリが扱っているフローは安定した公式インターフェースとは言い切れず、運用上・規約上ともにグレーな前提を含みます。将来的な仕様変更、`client_id` 制限、リダイレクト制約の強化などで突然動かなくなる可能性があります。

少なくとも以下の用途は推奨しません。

- 業務システムや本番環境への組み込み
- 商用 SaaS や複数ユーザー向けサービスでの利用
- 長期運用を前提にした自動化の認証基盤

個人検証やローカル実験の範囲で使い、利用可否やリスク判断は各自で行ってください。

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

このリポジトリは OpenAI OAuth client として動きます。Apps SDK の「自前認可サーバー」サンプルではありません。

`pi-mono` と同様に、ローカル CLI から OpenAI OAuth 認可を完了させる構成です。

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
export OPENAI_OAUTH_AUTH_FILE="$HOME/.config/codex-oauth-pkce/auth.json"
```

## 使い方

リポジトリ直下からそのまま実行できます。

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

既定では `os.UserConfigDir()/codex-oauth-pkce/auth.json` に保存します。`OPENAI_OAUTH_AUTH_FILE` で変更できます。

たとえば Linux では通常 `~/.config/codex-oauth-pkce/auth.json` です。

## CLIENT_ID 取得補助

`openai/codex` の GitHub code search から `CLIENT_ID` を引く補助コマンドも同梱しています。

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
