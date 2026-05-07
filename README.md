# kubepreview-controller

PRごとにプレビュー環境を自動作成するKubernetesコントローラー。

既存のDeployment/Serviceを複製し、Istio VirtualServiceによるヘッダーベースルーティングでプレビュー環境へのアクセスを提供します。

## 仕組み

`PreviewEnvironment` カスタムリソースを作成すると、コントローラーが以下を自動で行います：

1. 指定されたDeploymentを複製し、コンテナイメージをPR用に差し替え
2. 指定されたServiceを複製し、プレビュー用Podへ向ける
3. VirtualServiceを作成し、特定のHTTPヘッダーでプレビュー環境にルーティング
4. TTLを設定した場合、期限切れで自動削除

## 使い方

### サンプル

```yaml
apiVersion: preview.wow.one/v1alpha1
kind: PreviewEnvironment
metadata:
  name: my-preview
  namespace: staging
spec:
  identifier: "pr-123"
  image: "myapp:pr-123"
  deployments:
    - name: api-server
  services:
    - name: api-server
  replicas: 1
  ttl: "72h"
  routing:
    hosts:
      - "api-staging.example.com"
    gateways:
      - "istio-system/default-gateway"
    serviceName: "api-server"
    port: 80
```

上記を適用すると以下のリソースが作成されます：

- `Deployment/api-server-pr-123`（イメージ: `myapp:pr-123`）
- `Service/api-server-pr-123`
- `VirtualService/api-server-pr-123`（`x-preview-env: pr-123` ヘッダーでルーティング）

### プレビュー環境へのアクセス

リクエストに `x-preview-env` ヘッダーを付与するとプレビュー環境にルーティングされます：

```bash
curl -H "Host: api-staging.example.com" -H "x-preview-env: pr-123" http://<ingress-gateway>/
```

## 前提条件

- Go v1.24.6+
- kubectl v1.11.3+
- Kubernetes v1.11.3+ クラスタ
- Istio

## セットアップ

### ローカル開発

```bash
# CRDをクラスタにインストール
make install

# コントローラーをローカルで起動
make run
```

### クラスタへのデプロイ

```bash
# イメージをビルド・プッシュ
make docker-build docker-push IMG=<registry>/kubepreview-controller:<tag>

# デプロイ
make deploy IMG=<registry>/kubepreview-controller:<tag>
```

### アンインストール

```bash
make undeploy
make uninstall
```

## テスト

```bash
# ユニットテスト
make test

# E2Eテスト（Kindクラスタを使用）
make test-e2e
```

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
