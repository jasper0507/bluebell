<div align="center">

# Bluebell Backend

**基于 Go、Gin、MySQL 和 Redis 的社区论坛后端**

![Go](https://img.shields.io/badge/Go-1.27+-00ADD8?logo=go&logoColor=white)
![Gin](https://img.shields.io/badge/Gin-1.12-008ECF?logo=gin&logoColor=white)
![MySQL](https://img.shields.io/badge/MySQL-8.4-4479A1?logo=mysql&logoColor=white)
![Redis](https://img.shields.io/badge/Redis-7.4-DC382D?logo=redis&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-yellow.svg)

[快速开始](#快速开始) · [接口文档](./docs/openapi.yaml) · [配置](#配置) · [设计说明](#设计说明)

</div>

---

## 介绍

Bluebell 是一个类 Reddit 的社区论坛，本仓库是它的后端，提供 RESTful JSON API，配套前端见 [jasper0507/bluebell_frontend](https://github.com/jasper0507/bluebell_frontend)。

用户可以浏览社区、发帖、投票和评论，帖子列表支持按时间或热度排序。社区由管理者直接写入数据库，不提供创建接口。

## 特性

- **双令牌认证**：短期 JWT 加 HttpOnly Cookie 中的刷新令牌，刷新时原子轮换。
- **投票与热度排行**：赞成、反对、取消可随意切换，由 Redis Lua 脚本原子更新，发布 7 天后截止。
- **评论与回复**：评论可回复同帖的其他评论，平铺分页返回。
- **统一响应**：业务响应均为 `{code, message, data}`，错误码稳定。
- **完整的 OpenAPI 3.1 文档**：覆盖全部 17 个接口，可直接导入 Apifox、Postman。

## 技术栈

Go · [Gin](https://github.com/gin-gonic/gin) · [GORM](https://gorm.io/) · MySQL 8.4 · Redis 7.4（[go-redis](https://github.com/redis/go-redis)）· [golang-jwt](https://github.com/golang-jwt/jwt) + bcrypt · [Viper](https://github.com/spf13/viper) · `log/slog` · Docker Compose · [Air](https://github.com/air-verse/air)

代码按 `handler → service → repository / store` 分层，依赖在 `internal/app` 中通过构造函数注入。

## 快速开始

需要 Go 1.27+ 和 Docker。以下命令在仓库根目录执行。

```bash
git clone https://github.com/jasper0507/bluebell.git && cd bluebell

make up                                              # 启动 MySQL 和 Redis
cp configs/config.example.yaml configs/config.yaml   # 准备配置
make migrate                                         # 创建表（首次启动 MySQL 需等十几秒）
```

初始化几个社区（社区只能这样添加）：

```bash
docker compose exec -T mysql mysql -uapp -papp_password bluebell -e "
INSERT INTO communities (name, introduction, created_at, updated_at) VALUES
  ('Go', 'Go 语言开发交流', NOW(), NOW()),
  ('Database', '数据库与存储技术', NOW(), NOW());"
```

启动并验证：

```bash
make run                                      # 或 make dev 热重载（需先安装 air）
curl http://localhost:8080/api/v1/communities
```

> 配置模板里的 `auth.secret` 只是示例，非本地环境务必替换：`openssl rand -hex 32`。

## 配置

配置文件默认为 `configs/config.yaml`，字段说明见 [`config.example.yaml`](./configs/config.example.yaml)，可用 `BLUEBELL_CONFIG_FILE` 指定其他路径。

任何配置项都可以用环境变量覆盖：加 `BLUEBELL_` 前缀，`.` 换成 `_`，环境变量优先。适合部署时注入密钥：

```bash
export BLUEBELL_AUTH_SECRET="$(openssl rand -hex 32)"
export BLUEBELL_MYSQL_PASSWORD='your-password'
export BLUEBELL_AUTH_COOKIE_SECURE=true   # 生产环境启用 HTTPS 后开启
```

## 常用命令

| 命令                    | 说明                       |
| ----------------------- | -------------------------- |
| `make up` / `make down` | 启动 / 停止 MySQL 和 Redis |
| `make migrate`          | 数据库迁移                 |
| `make run` / `make dev` | 启动服务 / 热重载启动      |
| `make build`            | 编译到 `bin/bluebell`      |
| `make check`            | 格式化、`go vet` 和测试    |

`make test` 会跑全部测试。Token 的校验不依赖外部服务。`service` 里的登录和投票测试使用 MySQL 库 `bluebell_test` 与 Redis DB 14，`store` 里的 Refresh Token 和投票测试使用 Redis DB 15。它们连接 `make up` 启动的 MySQL 和 Redis，不会改动开发库 `bluebell`。

## 设计说明

**数据分工**：用户、社区、帖子、评论存 MySQL；投票统计、排行榜、刷新令牌存 Redis。帖子列表先从 Redis 排行榜取出当前页 ID，再批量回查 MySQL，并合并票数。

**热度算法**：借鉴 Reddit，`net = 赞成数 − 反对数`：

```text
hot = sign(net) × log10(max(|net|, 1)) + (created_at − epoch) / 45000
```

票数按对数增长，发布时间越新分数越高，`epoch` 为 `2026-01-01 00:00:00 UTC`。

**令牌**：`access_token` 为无状态 JWT，默认 15 分钟；`refresh_token` 默认 7 天，Redis 中只存其哈希，轮换后新令牌沿用旧令牌的剩余有效期，因此不会无限续期。

## 已知限制与后续优化

- **Redis 是排序和票数的数据源**：Redis 数据丢失后，已有帖子不会出现在列表中，票数也无法恢复。后续计划通过 Redis 持久化和 Outbox 模式优化。
- **没有 CORS 中间件**：浏览器跨域调用需要开发代理或网关处理。
- **`/health` 不检查依赖**。

## 维护接口文档

[`docs/openapi.yaml`](./docs/openapi.yaml) 是接口定义的唯一入口，改接口时同步修改它，并以实际行为为准：路由看 [`router.go`](./internal/router/router.go)，字段和状态码看 [`handler`](./internal/handler/)，错误码看 [`code.go`](./internal/response/code.go)。修改后校验（需要 [uv](https://docs.astral.sh/uv/)）：

```bash
uvx --from openapi-spec-validator==0.7.2 openapi-spec-validator docs/openapi.yaml
```

## 许可证

[MIT](./LICENSE)
