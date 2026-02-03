# 基础镜像
FROM golang:1.23-alpine AS builder

# 安装必要的构建工具
RUN apk add --no-cache git make

# 设置工作目录
WORKDIR /app

# 复制依赖文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 编译
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o match-engine ./cmd/server

# 运行镜像
FROM alpine:3.19

# 安装必要的运行时依赖
RUN apk --no-cache add ca-certificates tzdata

# 设置时区
ENV TZ=Asia/Shanghai

# 创建非 root 用户
RUN adduser -D -g '' appuser

# 设置工作目录
WORKDIR /app

# 从 builder 复制二进制文件
COPY --from=builder /app/match-engine .

# 创建快照目录
RUN mkdir -p /app/snapshots && chown -R appuser:appuser /app

# 切换到非 root 用户
USER appuser

# 暴露端口
# gRPC
EXPOSE 50051
# WebSocket
EXPOSE 8081
# Prometheus Metrics
EXPOSE 9090

# 健康检查
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8081/health || exit 1

# 启动命令
ENTRYPOINT ["./match-engine"]
CMD ["-config", "config.json"]
