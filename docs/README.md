# Bluebell 接口文档

[openapi.yaml](./openapi.yaml) 是接口定义的唯一维护入口，采用 [OpenAPI 3.1.1](https://spec.openapis.org/oas/v3.1.1.html)，覆盖当前全部 14 个接口。参数、响应、错误码、业务规则和示例均在文件内。

## 导入与联调

在支持 OpenAPI 3.1 的接口工具中直接导入 `docs/openapi.yaml`，无需生成步骤。使用工具的环境配置将服务地址设为实际后端地址，默认是 `http://localhost:8080`。文档中的路径已经包含 `/api/v1`，服务地址不要再追加此前缀。

后端监听地址由 `http.addr` 配置，示例见 [config.example.yaml](../configs/config.example.yaml)。远程联调请使用后端同学提供的地址；修改文档或工具中的地址不会修改后端监听配置。

社区、帖子查询和评论列表是公开接口，无需令牌即可调用；发帖、删帖、投票、发表评论和删除评论需要登录。

1. 使用已有账号登录，或先调用注册接口。
2. 从登录响应的 `data.access_token` 取出令牌。
3. 在工具的 HTTP Bearer 鉴权输入框中填写令牌；直接发请求时，携带 `Authorization: Bearer <access_token>`。
4. 需要登录的接口收到 `401 / UNAUTHORIZED` 后重新登录。有效期和各接口的鉴权要求见 YAML。

已有账号可这样登录：

```bash
curl -X POST 'http://localhost:8080/api/v1/users/login' \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"Example123"}'
```

公开接口可直接调用，例如查询社区：

```bash
curl 'http://localhost:8080/api/v1/communities'
```

将返回的令牌填入下方占位位置，再调用需要登录的接口，例如发帖：

```bash
curl -X POST 'http://localhost:8080/api/v1/posts' \
  -H 'Authorization: Bearer <access_token>' \
  -H 'Content-Type: application/json' \
  -d '{"title":"Go 错误处理实践","content":"讨论一下项目中如何处理错误。","community_id":1}'
```

当前后端未配置 CORS。浏览器中的前端或文档页面跨域调用时，需要通过开发代理或部署网关处理；导入 OpenAPI 文件本身不会开启跨域支持。

## 维护与校验

接口变更时，同步修改 `openapi.yaml`：路由以 [router.go](../internal/router/router.go) 为准，字段和状态码核对 [handler](../internal/handler/)，业务规则核对 [service](../internal/service/) 与 [repository](../internal/repository/)，错误码核对 [code.go](../internal/response/code.go)。文档记录实际行为，发现实现问题时另行修复，避免先在文档里承诺尚未实现的能力。

安装了 `uv` 的环境，可在仓库根目录使用临时工具校验，无需向项目添加依赖：

```bash
uvx --from openapi-spec-validator==0.7.2 openapi-spec-validator docs/openapi.yaml
```

该命令检查 OpenAPI 结构及引用。提交前还需核对示例与 schema、路由覆盖、鉴权范围及错误分支；规范校验不能替代与实际后端的联调。
