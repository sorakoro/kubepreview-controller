# kubepreview-controller

A Kubernetes controller that automatically creates preview environments per pull request.

It clones existing Deployments and Services, then provides access to preview environments via header-based routing using Istio VirtualService.

## How It Works

When you create a `PreviewEnvironment` custom resource, the controller automatically:

1. Clones the specified Deployment and replaces the container image with the PR version
2. Clones the specified Service and points it to the preview Pods
3. Creates a VirtualService that routes traffic to the preview environment based on a specific HTTP header
4. Automatically deletes the environment when the TTL expires (if configured)

## Usage

### Example

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

Applying the above creates the following resources:

- `Deployment/api-server-pr-123` (image: `myapp:pr-123`)
- `Service/api-server-pr-123`
- `VirtualService/api-server-pr-123` (routes via `x-preview-env: pr-123` header)

### Accessing Preview Environments

Add the `x-preview-env` header to your request to route traffic to the preview environment:

```bash
curl -H "Host: api-staging.example.com" -H "x-preview-env: pr-123" http://<ingress-gateway>/
```

## Prerequisites

- Go v1.26.0+
- kubectl v1.34.0+
- Kubernetes v1.34.0+ cluster
- Istio v1.29+

## Setup

### Local Development

```bash
# Install CRDs into the cluster
make install

# Run the controller locally
make run
```

### Install from Release Manifest

```bash
kubectl apply -f https://github.com/sorakoro/kubepreview-controller/releases/latest/download/install.yaml
```

### Build and Deploy from Source

```bash
# Build and push the image
make docker-build docker-push IMG=<registry>/kubepreview-controller:<tag>

# Deploy
make deploy IMG=<registry>/kubepreview-controller:<tag>
```

### Uninstall

```bash
make undeploy
make uninstall
```

## Testing

```bash
# Unit tests
make test

# E2E tests (uses a Kind cluster)
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
