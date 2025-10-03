# PodController 实现文档

## 概述

PodController 是 Apollo Network Controller 中负责管理 Pod 端口分配的控制器。它监控 Pod 资源，并根据 Pod 上的特定注解自动分配和释放端口。

## 功能特性

1. **自动端口分配**: 当 Pod 被创建且带有正确的注解时，自动为其分配端口
2. **自动端口释放**: 当 Pod 被删除时，自动释放已分配的端口
3. **重复分配防护**: 防止同一个 Pod 在同一个 Pool 中重复分配端口
4. **智能端口解析**: 按优先级从注解或 Pod Spec 解析容器端口配置
5. **容器无端口声明支持**: 支持容器不在 Pod Spec 中声明端口的场景

## Pod 注解说明

PodController 通过以下注解来识别和管理需要端口分配的 Pod：

### 必需注解

- `apollo.magicsong.io/enable-allocation`: 设置为 `"true"` 启用端口分配
- `apollo.magicsong.io/network-pool`: 指定要使用的网络池名称（支持多种格式）

### 可选注解

- `apollo.magicsong.io/container-ports`: JSON 格式的容器端口配置（**推荐使用**）

## 多网络池支持

从v1.1开始，PodController支持为单个Pod从多个网络池分配端口。这在以下场景中很有用：
- Pod需要从不同类型的网络池获取端口（如内网和外网）
- 实现端口分配的高可用性和负载分散
- 渐进式网络池迁移

### 网络池配置格式

`apollo.magicsong.io/network-pool` 注解支持以下格式：

#### 1. 单个网络池（字符串格式）
```yaml
apollo.magicsong.io/network-pool: "default-pool"
```

#### 2. 多个网络池（逗号分隔格式）
```yaml
apollo.magicsong.io/network-pool: "pool1,pool2,pool3"
```

#### 3. 多个网络池（JSON数组格式，推荐）
```yaml
apollo.magicsong.io/network-pool: '["pool1","pool2","pool3"]'
```

### 多池分配行为

- 系统会依次尝试从每个指定的网络池分配端口
- 只要任何一个池分配成功，就认为分配成功
- 如果所有池都分配失败，才会返回错误
- 端口释放时会自动清理所有相关的分配

## 端口解析优先级

PodController 按以下优先级解析容器端口：

1. **注解配置**（最高优先级）: `apollo.magicsong.io/container-ports`
   - 推荐使用此方式，因为容器可能不会在 Pod Spec 中声明端口
   - 更精确和可控的端口配置
   
2. **Pod Spec 端口**（第二优先级）: 从 `pod.spec.containers[].ports` 中提取
   - 仅在没有注解配置时使用
   - 依赖容器在 Pod Spec 中正确声明端口
   
3. **空端口列表**（最低优先级）: 当以上两种方式都没有找到端口时
   - 允许分配空的端口列表，适用于动态端口或无需负载均衡的场景

## 使用示例

### 基本使用

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: web-app
  namespace: default
  annotations:
    apollo.magicsong.io/enable-allocation: "true"
    apollo.magicsong.io/network-pool: "production-pool"
spec:
  containers:
  - name: web
    image: nginx:latest
    ports:
    - containerPort: 80
      name: http
      protocol: TCP
    - containerPort: 443
      name: https
      protocol: TCP
```

### 使用自定义端口配置（推荐）

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: api-server
  namespace: default
  annotations:
    apollo.magicsong.io/enable-allocation: "true"
    apollo.magicsong.io/network-pool: "api-pool"
    # 推荐：使用注解明确指定端口，避免依赖容器端口声明
    apollo.magicsong.io/container-ports: |
      [
        {
          "podPort": 8080,
          "protocol": "TCP",
          "portName": "http-api"
        },
        {
          "podPort": 9090,
          "protocol": "TCP",
          "portName": "metrics"
        }
      ]
spec:
  containers:
  - name: api
    image: my-api:latest
    # 注意：容器可能不声明端口，但仍然监听这些端口
```

### 容器无端口声明的场景

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: microservice
  namespace: default
  annotations:
    apollo.magicsong.io/enable-allocation: "true"
    apollo.magicsong.io/network-pool: "production-pool"
    # 必须使用注解指定端口，因为容器Spec中没有声明端口
    apollo.magicsong.io/container-ports: |
      [
        {
          "podPort": 3000,
          "protocol": "TCP",
          "portName": "service"
        }
      ]
spec:
  containers:
  - name: microservice
    image: node-app:latest
    # 容器监听3000端口，但在Spec中未声明
```

### 多网络池使用场景

#### 场景1：高可用性配置
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: high-availability-app
  namespace: default
  annotations:
    apollo.magicsong.io/enable-allocation: "true"
    # JSON数组格式（推荐）
    apollo.magicsong.io/network-pool: '["primary-pool","backup-pool","fallback-pool"]'
    apollo.magicsong.io/container-ports: |
      [
        {
          "podPort": 8080,
          "protocol": "TCP",
          "portName": "http"
        }
      ]
spec:
  containers:
  - name: app
    image: my-app:latest
```

#### 场景2：混合网络环境
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: hybrid-service
  namespace: default
  annotations:
    apollo.magicsong.io/enable-allocation: "true"
    # 逗号分隔格式
    apollo.magicsong.io/network-pool: "internal-pool,external-pool"
    apollo.magicsong.io/container-ports: |
      [
        {
          "podPort": 8080,
          "protocol": "TCP",
          "portName": "public-api"
        },
        {
          "podPort": 9090,
          "protocol": "TCP",
          "portName": "internal-metrics"
        }
      ]
spec:
  containers:
  - name: service
    image: hybrid-service:latest
```

## 工作流程

### Pod 创建流程

1. PodController 监测到新 Pod 创建
2. 检查 Pod 是否包含必需的注解
3. 如果包含注解，解析容器端口配置
4. 检查是否已存在端口分配
5. 如果不存在，调用 PoolAllocator 分配端口
6. 创建 PortAllocation 资源记录分配信息

### Pod 删除流程

1. PodController 监测到 Pod 被标记为删除
2. 检查 Pod 是否包含必需的注解
3. 如果包含注解，调用 PoolAllocator 释放端口
4. 删除对应的 PortAllocation 资源

## 错误处理

- **缺少注解**: 跳过处理，不分配端口
- **网络池不存在**: 记录错误，返回失败
- **端口配置无效**: 记录错误，返回失败
- **端口分配失败**: 记录错误，可能稍后重试
- **重复分配**: 跳过分配，保持现有分配

## 日志级别

- **Info**: 重要操作（分配成功、释放成功）
- **V(1)**: 调试信息（Pod 检查、解析端口等）
- **Error**: 错误情况（分配失败、释放失败等）

## 监控和观察

PodController 会产生以下类型的日志：

- `"Reconciling Pod"`: Pod 开始处理
- `"Pod not marked for port allocation, skipping"`: Pod 没有相关注解
- `"Pod is being deleted, releasing ports"`: Pod 删除，释放端口
- `"Pod is active, ensuring port allocation"`: 为活跃 Pod 确保端口分配
- `"Successfully allocated ports for pod"`: 端口分配成功
- `"Successfully released ports for pod"`: 端口释放成功

## 与其他组件的关系

- **PoolAllocator**: 实际执行端口分配和释放逻辑
- **ApolloNetworkPool**: 提供网络池配置和负载均衡器信息
- **PortAllocation**: 记录端口分配状态和绑定信息
- **ApolloNetworkPoolController**: 更新网络池状态，反映端口使用情况

## 最佳实践

1. **优先使用注解**: 始终使用 `apollo.magicsong.io/container-ports` 注解明确指定端口，不要依赖容器在 Pod Spec 中的端口声明
2. **注解使用**: 只为需要外部访问的 Pod 添加端口分配注解
3. **网络池选择**: 根据 Pod 的网络需求选择合适的网络池
4. **多池配置**: 
   - 使用JSON数组格式配置多个网络池（推荐）
   - 按优先级排列网络池（第一个为首选）
   - 避免配置过多网络池以减少分配复杂度
5. **端口规划**: 避免在容器端口配置中使用冲突的端口
6. **容器端口管理**: 记住容器可能监听端口但不在 Pod Spec 中声明，依赖注解配置
7. **资源清理**: 确保 Pod 删除时能够正确清理端口分配
8. **监控告警**: 监控端口分配失败的情况，及时处理资源不足问题

## 故障排查

### 常见问题

1. **端口分配失败**
   - 检查网络池是否存在
   - 检查网络池是否有足够的可用端口
   - 检查负载均衡器配置是否正确

2. **重复分配错误**
   - 检查是否已存在相同 Pod 的分配
   - 检查 PortAllocation 资源状态

3. **端口释放失败**
   - 检查 PortAllocation 资源是否存在
   - 检查删除权限是否正确

### 调试命令

```bash
# 查看 Pod 的注解
kubectl get pod <pod-name> -o jsonpath='{.metadata.annotations}'

# 查看端口分配资源
kubectl get portallocation -l apollo.magicsong.io/pod-name=<pod-name>

# 查看控制器日志
kubectl logs -n apollo-network-controller-system deployment/apollo-network-controller-manager
```