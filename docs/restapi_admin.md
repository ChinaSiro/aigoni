# restapi_admin

面向 `frontend/admin` 的后台 REST API 当前契约。

实现入口：`internal/server/admin_api*.go`、`internal/server/settings_api.go`、`internal/server/admin_wiki_api.go`；路由入口：`internal/server/admin_api_routes.go`。

## 使用规则

- 基础路径固定为 `/api/admin/v1`；后台页面访问路径由 `ADMIN_PATH` 配置，不改变 API 基础路径。
- 除登录接口外，所有接口都要求管理员 Session Cookie。
- 所有 `POST`、`PUT`、`PATCH`、`DELETE` 都必须携带同源 Cookie 和 `X-CSRF-Token`。
- JSON 请求使用 `Content-Type: application/json`，响应使用 `application/json; charset=utf-8`。
- JSON 请求体只能包含一个 JSON 值；未知字段、非法 JSON、尾随 JSON 值和超限请求必须被拒绝。
- 普通 JSON 请求体上限 `2 MiB`，登录请求体上限 `1 MiB`；文件和远程图片上限 `20 MiB`。
- 文件接口使用 `multipart/form-data`；SSE 接口使用 `text/event-stream`。
- API 错误必须返回 JSON，不得重定向到登录页，不得返回 SPA HTML。
- `/api/content` 是独立的 API Key 机器写入接口，不属于本契约。

## 表格说明

每个 URL 单独列出请求字段表和响应字段表：

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `field` | `GET`、`POST`、`PATCH` 等 | 不可空、可为空、条件必填或不可用 | 示例值 | 字段用途、校验和默认值。 |

## POST /api/admin/v1/session

登录并创建管理员 Session；重复登录会轮换 Session Cookie，响应中的 `csrf_token` 始终对应本次新 Session。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `password` | POST | 不可空 | `"secret"` | 去除空白后不可为空。 |
| 请求体 | POST | 不可空 | `{"password":"secret"}` | 必须是单个 JSON 对象，最大 `1 MiB`；不需要 Session 和 CSRF。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `authenticated` | POST | 不可空 | `true` | 登录是否成功。 |
| `expires_at` | POST | 不可空 | `"2026-08-04T12:00:00Z"` | Session 过期时间，RFC3339。 |
| `capabilities` | POST | 不可空 | `["admin"]` | 管理能力数组。 |
| `csrf_token` | POST | 不可空 | `"csrf-token"` | 后续所有写请求使用 `X-CSRF-Token` 发送。 |

错误密码返回 `401`；JSON 不合法、未知字段或尾随 JSON 值返回 `400`。

## GET /api/admin/v1/session

查询当前管理员 Session。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | GET | 不可空 | `aigoni_session=...` | 必须是有效 Session。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `authenticated` | GET | 不可空 | `true` | 当前是否已认证。 |
| `expires_at` | GET | 不可空 | `"2026-08-04T12:00:00Z"` | Session 过期时间。 |
| `capabilities` | GET | 不可空 | `["admin"]` | 管理能力数组。 |
| `csrf_token` | GET | 不可空 | `"csrf-token"` | 当前 Session 的 CSRF Token。 |

无效 Session 返回 `401`。

## DELETE /api/admin/v1/session

退出管理员 Session。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | DELETE | 不可空 | `aigoni_session=...` | 必须是有效 Session。 |
| `X-CSRF-Token` | DELETE | 不可空 | `csrf-token` | 必须匹配当前 Session。 |
| 请求体 | DELETE | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| 响应体 | DELETE | 不可用 | 空 | 成功返回 `204 No Content`。 |

## GET /api/admin/v1/dashboard

返回后台首页统计和功能状态。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | GET | 不可空 | `aigoni_session=...` | 必须已登录。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `posts` | GET | 不可空 | `{"total":10,"published":8,"draft":2}` | 文章统计对象。 |
| `posts.total`、`published`、`draft` | GET | 不可空 | `10` | 文章总数、已发布数、草稿数。 |
| `pages` | GET | 不可空 | `{"total":2,"published":2,"draft":0}` | 页面统计对象。 |
| `pages.total`、`published`、`draft` | GET | 不可空 | `2` | 页面统计。 |
| `notes` | GET | 不可空 | `5` | 私人笔记数量。 |
| `features` | GET | 不可空 | `{"posts":true}` | 后台功能开关对象。 |

## GET /api/admin/v1/{type}

内容分页列表。`{type}` 只能是 `posts`、`pages` 或 `notes`。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | GET | 不可空 | `aigoni_session=...` | 必须已登录。 |
| `page` | GET | 可为空 | `1` | 默认 `1`；非法值回退 `1`。 |
| `per_page` | GET | 可为空 | `20` | 默认 `20`，最大 `100`。 |
| `publish` | GET | 可为空 | `true` | `posts`、`pages` 可用；筛选已发布或草稿。 |
| `category` | GET | 可为空 | `"Go"` | `posts`、`pages`、`notes` 均可用；传 `__none__` 筛选分类为空的条目。 |
| `tag` | GET | 可为空 | `"API"` | `posts`、`pages`、`notes` 均可用。 |
| 请求体 | GET | 不可用 | 空 | 所有条件通过 Query 提交。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `items` | GET | 不可空 | `[]` | 当前页内容数组。 |
| `page`、`per_page`、`total`、`total_pages` | GET | 不可空 | `1` | 分页字段。 |
| `items[].id` | GET | 不可空 | `"2026/2026-08-04-1"` | 稳定内容 ID，可能包含 `/`；笔记只保留 `年/日期-数字ID` 前缀。 |
| `items[].path`、`type`、`title`、`date`、`lastmod` | GET | 不可空 | - | 基础内容字段；日期为 RFC3339；笔记 path 带标题后缀。 |
| `items[].description`、`category`、`cover_image`、`source_url`、`template` | GET | 可为空 | `"Go"` | 无值时为空字符串。 |
| `items[].publish`、`toc`、`weight` | GET | 不可空 | `true` | 笔记对应字段按私密规则返回默认值。 |
| `items[].tags` | GET | 不可空 | `[]` | 始终为数组。 |
| `items[].revision` | GET | 不可空 | `"sha256..."` | 当前文件内容 revision。 |
| `items[].edit_url` | GET | 可为空 | `"/api/admin/v1/posts/2026/..."` | 后台编辑地址。 |
| `items[].body`、`items[].html` | GET | 不可用 | - | 列表不返回正文和 HTML。 |

## POST /api/admin/v1/{type}

创建文章、页面或笔记。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | POST | 不可空 | `aigoni_session=...` | 必须已登录。 |
| `X-CSRF-Token` | POST | 不可空 | `csrf-token` | 必须匹配当前 Session。 |
| `title` | POST | 条件必填 | `"通过 API 发布"` | 文章和页面必填；笔记可空，但不能与 `body` 同时为空。 |
| `description` | POST | 可为空 | `"文章摘要"` | 可省略。 |
| `date` | POST | 不可空 | `"2026-08-04T00:00:00Z"` | 所有类型必填，必须是 RFC3339。 |
| `publish` | POST | 可为空 | `true` | 文章和页面可用；默认 `false`；笔记禁止。 |
| `slug` | POST | 条件必填 | `"rest-api"` | 文章和页面必填；笔记禁止。 |
| `category` | POST | 可为空 | `"Go"` | 可省略。 |
| `tags` | POST | 可为空 | `["API"]` | 可省略；数组。 |
| `cover_image` | POST | 可为空 | `"/assets/posts/cover.png"` | 文章和页面可用；笔记禁止。 |
| `toc` | POST | 可为空 | `true` | 文章和页面可用；默认 `false`；笔记禁止。 |
| `template` | POST | 可为空 | `"legacy.html"` | 历史元数据兼容字段；不触发模板渲染；笔记禁止。 |
| `source_url` | POST | 可为空 | `"https://example.com"` | 可省略。 |
| `weight` | POST | 可为空 | `10` | 文章和页面可用；默认 `0`；笔记禁止。 |
| `body` | POST | 条件必填 | `"## 正文"` | 笔记至少要有 `title` 或 `body`；其他类型可按内容校验。 |
| `draft_token` | POST | 可为空 | `"draft-token"` | 有草稿资源时提交，格式必须合法。 |
| 请求体 | POST | 不可空 | `{...}` | 必须是单个 JSON 对象，最大 `2 MiB`；未知字段和尾随 JSON 值返回 `400`。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `id`、`path`、`type`、`title`、`date`、`lastmod` | POST | 不可空 | - | 创建后的内容基础字段。 |
| `description`、`category`、`source_url`、`template` | POST | 可为空 | `"Go"` | 无值时为空字符串。 |
| `publish`、`toc`、`weight` | POST | 不可空 | `true` | 内容状态字段。 |
| `slug` | POST | 可为空 | `"rest-api"` | 笔记不返回有效 slug。 |
| `tags` | POST | 不可空 | `[]` | 始终为数组。 |
| `body`、`html` | POST | 不可空 | - | 保存后的正文和渲染 HTML。 |
| `revision` | POST | 不可空 | `"sha256..."` | 同时写入 `ETag`。 |
| `saved_at` | POST | 不可空 | `"2026-08-04T00:00:00Z"` | 保存时间，RFC3339。 |
| `edit_url`、`asset_prefix` | POST | 不可空 | - | 后台编辑地址和资源前缀。 |
| `Location` 响应头 | POST | 不可空 | `/api/admin/v1/posts/...` | 创建成功返回 `201`。 |

## GET /api/admin/v1/{type}/{id}

读取内容详情。`{id}` 可能包含 `/`，禁止 `..`、反斜杠和绝对路径。笔记的 `{id}` 是稳定的 `年/日期-数字ID` 前缀，实际 `path` 还包含标题后缀。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | GET | 不可空 | `aigoni_session=...` | 必须已登录。 |
| `type` | GET | 不可空 | `posts` | 只能是 `posts`、`pages`、`notes`。 |
| `id` | GET | 不可空 | `2026/2026-08-04-1` | URL 路径中的稳定内容 ID。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `id`、`path`、`type`、`title`、`date`、`lastmod` | GET | 不可空 | - | 内容基础字段。 |
| `description`、`category`、`cover_image`、`source_url`、`template` | GET | 可为空 | `"Go"` | 无值时为空字符串。 |
| `publish`、`toc`、`weight`、`tags`、`revision` | GET | 不可空 | - | 状态、标签和版本字段。 |
| `body`、`html` | GET | 不可空 | - | 完整正文和渲染 HTML。 |
| `edit_url` | GET | 可为空 | `"/api/admin/v1/posts/..."` | 后台编辑地址。 |

不存在返回 `404`。

## PATCH /api/admin/v1/{type}/{id}

部分更新内容。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | PATCH | 不可空 | `aigoni_session=...` | 必须已登录。 |
| `X-CSRF-Token` | PATCH | 不可空 | `csrf-token` | 必须匹配当前 Session。 |
| `If-Match` | PATCH | 可为空 | `"sha256..."` | 优先于 JSON `revision`；可省略。 |
| `revision` | PATCH | 可为空 | `"sha256..."` | 未提供 `If-Match` 时使用；可省略。 |
| `title`、`description`、`date`、`publish`、`slug`、`category`、`tags`、`cover_image`、`toc`、`template`、`source_url`、`weight`、`body` | PATCH | 可为空 | - | 只修改请求中出现的字段；字段校验同创建接口。 |
| 请求体 | PATCH | 不可空 | `{"body":"更新"}` | 单个 JSON 对象，最大 `2 MiB`；未知字段和尾随 JSON 值返回 `400`。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `id`、`path`、`type`、`title`、`date`、`lastmod`、`body`、`html` | PATCH | 不可空 | - | 更新后的完整内容；笔记修改标题时 `id` 前缀不变，`path` 会同步更新。 |
| `description`、`category`、`cover_image`、`source_url`、`template` | PATCH | 可为空 | - | 无值时为空字符串。 |
| `tags`、`revision`、`saved_at`、`edit_url`、`asset_prefix` | PATCH | 不可空 | - | 更新后的标签、版本和操作地址。 |
| `ETag` 响应头 | PATCH | 不可空 | `"sha256..."` | 新 revision。 |

旧 revision 返回 `409 revision_conflict`。

## DELETE /api/admin/v1/{type}/{id}

删除内容。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | DELETE | 不可空 | `aigoni_session=...` | 必须已登录。 |
| `X-CSRF-Token` | DELETE | 不可空 | `csrf-token` | 必须匹配当前 Session。 |
| `If-Match` | DELETE | 可为空 | `"sha256..."` | 可省略；提供时必须匹配当前版本。 |
| 请求体 | DELETE | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| 响应体 | DELETE | 不可用 | 空 | 成功返回 `204`；版本冲突返回 `409`。 |

## GET /api/admin/v1/categories

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | GET | 不可空 | `aigoni_session=...` | 必须已登录。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| 响应体 | GET | 不可空 | `[]` | 直接返回数组，不使用 `items` 包装。 |
| `[].name` | GET | 不可空 | `"Go"` | 文章分类名称。 |
| `[].count` | GET | 不可空 | `3` | 文章数量。 |
| `[].none` | GET | 可为空 | `true` | 内置"未分类"项标记；仅在数组首个元素为 `{name:"未分类",none:true}` 时存在，表示分类为空的文章数量；列表筛选时对应的 category 值为 `__none__`。 |

## GET /api/admin/v1/note-categories

结构与文章分类相同，但数据来源是笔记。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | GET | 不可空 | `aigoni_session=...` | 必须已登录。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| 响应体 | GET | 不可空 | `[]` | 直接返回数组。 |
| `[].name`、`[].count` | GET | 不可空 | `"Research"`、`2` | 笔记分类名称和数量。 |
| `[].none` | GET | 可为空 | `true` | 内置"未分类"项标记；仅在数组首个元素为 `{name:"未分类",none:true}` 时存在，表示分类为空的笔记数量；列表筛选时对应的 category 值为 `__none__`。 |

## GET /api/admin/v1/tags

返回所有文章的标签大全，供后台筛选下拉使用。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | GET | 不可空 | `aigoni_session=...` | 必须已登录。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| 响应体 | GET | 不可空 | `[]` | 直接返回数组，不使用 `items` 包装。 |
| `[].name` | GET | 不可空 | `"API"` | 文章标签名称。 |
| `[].count` | GET | 不可空 | `3` | 使用该标签的文章数量。 |

## GET /api/admin/v1/note-tags

返回所有笔记的标签大全，供后台筛选下拉使用。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | GET | 不可空 | `aigoni_session=...` | 必须已登录。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| 响应体 | GET | 不可空 | `[]` | 直接返回数组。 |
| `[].name`、`[].count` | GET | 不可空 | `"灵感"`、`2` | 笔记标签名称和数量。 |

## GET /api/admin/v1/search?q={keyword}

跨文章、页面和笔记搜索。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | GET | 不可空 | `aigoni_session=...` | 必须已登录。 |
| `q` | GET | 不可空 | `"Markdown"` | 去除空白后不可为空；否则返回 `400`。 |
| `type` | GET | 可为空 | `post` | 可选 `post`、`page`、`note`；省略时搜索全部。 |
| `page` | GET | 可为空 | `1` | 默认 `1`。 |
| `per_page` | GET | 可为空 | `20` | 默认 `20`，最大 `100`。 |
| 请求体 | GET | 不可用 | 空 | 所有条件通过 Query 提交。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `items`、`page`、`per_page`、`total`、`total_pages` | GET | 不可空 | - | 分页字段。 |
| `items[].excerpt` | GET | 可为空 | `"命中摘要"` | 命中摘要；为空时可省略。 |
| `items[].id`、`type`、`title`、`date`、`revision` | GET | 不可空 | - | 搜索结果基础字段。 |
| `items[].edit_url` | GET | 可为空 | `"/api/admin/v1/posts/..."` | 编辑地址。 |

## POST /api/admin/v1/drafts

创建临时草稿资源区。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | POST | 不可空 | `aigoni_session=...` | 必须已登录。 |
| `X-CSRF-Token` | POST | 不可空 | `csrf-token` | 必须匹配当前 Session。 |
| 请求体 | POST | 可省略 | `{}` | 不接受业务字段；前端通常发送空对象。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `token` | POST | 不可空 | `"draft-token"` | 草稿 Token。 |
| `asset_prefix` | POST | 不可空 | `"/assets/.drafts/draft-token.assets"` | 草稿资源路径前缀。 |
| `cleanup` | POST | 不可空 | `"removed when the draft is committed; abandoned drafts older than 7 days are cleaned automatically"` | 提交后迁移；放弃超过 7 天自动清理。 |

成功返回 `201`。

## GET /api/admin/v1/drafts/{token}/assets

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | GET | 不可空 | `aigoni_session=...` | 必须已登录。 |
| `token` | GET | 不可空 | `draft-token` | 必须符合草稿 Token 格式。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `items` | GET | 不可空 | `[]` | 资源数组。 |
| `cover` | GET | 可为空 | `null` | 没有封面时为 `null`。 |
| `items[].name`、`path`、`markdown`、`is_image`、`is_cover`、`size` | GET | 不可空 | - | 资源元数据。 |

## POST /api/admin/v1/drafts/{token}/assets

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | POST | 不可空 | `aigoni_session=...` | 必须已登录。 |
| `X-CSRF-Token` | POST | 不可空 | `csrf-token` | 必须匹配当前 Session。 |
| `token` | POST | 不可空 | `draft-token` | 路径字段。 |
| `asset` | POST | 不可空 | `image.png` | `multipart/form-data` 文件字段；最大 `20 MiB`。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `name`、`path`、`markdown`、`is_image`、`is_cover`、`size` | POST | 不可空 | - | 上传后的资源对象；成功 `201`。 |

## PUT /api/admin/v1/drafts/{token}/assets/cover

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie、`X-CSRF-Token`、`token` | PUT | 不可空 | - | Session、CSRF 和草稿 Token 必须有效。 |
| `cover` | PUT | 不可空 | `cover.png` | `multipart/form-data` 文件字段；最大 `20 MiB`。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `name`、`path`、`markdown`、`is_image`、`is_cover`、`size` | PUT | 不可空 | - | 上传后的封面对象；成功 `200`。 |

## DELETE /api/admin/v1/drafts/{token}/assets/cover

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie、`X-CSRF-Token`、`token` | DELETE | 不可空 | - | 必须已登录并通过 CSRF。 |
| 请求体 | DELETE | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| 响应体 | DELETE | 不可用 | 空 | 成功返回 `204`。 |

## DELETE /api/admin/v1/drafts/{token}/assets/{name}

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie、`X-CSRF-Token` | DELETE | 不可空 | - | 必须通过 Session 和 CSRF。 |
| `token`、`name` | DELETE | 不可空 | `draft-token`、`image.png` | URL 解码后校验；禁止目录穿越和删除受保护文件。 |
| 请求体 | DELETE | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| 响应体 | DELETE | 不可用 | 空 | 成功返回 `204`；资源不存在返回 `404`。 |

## 正式内容资源 URL

以下 `{type}` 只能是 `posts`、`pages`、`notes`，`{id}` 为稳定内容 ID。正式资源接口与草稿接口字段相同，区别是资源归属正式内容。

### GET /api/admin/v1/{type}/{id}/assets

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie、`type`、`id` | GET | 不可空 | - | 必须已登录；`id` 禁止 `..`、反斜杠和绝对路径。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |
| `items` | GET | 不可空 | `[]` | 正式资源数组。 |
| `cover` | GET | 可为空 | `null` | 没有封面时为 `null`。 |

### POST /api/admin/v1/{type}/{id}/assets

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie、`X-CSRF-Token`、`type`、`id` | POST | 不可空 | - | 必须通过 Session 和 CSRF。 |
| `asset` | POST | 不可空 | `image.png` | `multipart/form-data`；最大 `20 MiB`。 |
| `name`、`path`、`markdown`、`is_image`、`is_cover`、`size` | POST | 不可空 | - | 响应资源对象；成功 `201`。 |

### PUT /api/admin/v1/{type}/{id}/assets/cover

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie、`X-CSRF-Token`、`type`、`id` | PUT | 不可空 | - | 必须通过 Session 和 CSRF。 |
| `cover` | PUT | 不可空 | `cover.png` | `multipart/form-data`；最大 `20 MiB`。 |
| `name`、`path`、`markdown`、`is_image`、`is_cover`、`size` | PUT | 不可空 | - | 响应资源对象；成功 `200`。 |

### DELETE /api/admin/v1/{type}/{id}/assets/cover

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie、`X-CSRF-Token`、`type`、`id` | DELETE | 不可空 | - | 必须通过 Session 和 CSRF。 |
| 响应体 | DELETE | 不可用 | 空 | 成功返回 `204`。 |

### DELETE /api/admin/v1/{type}/{id}/assets/{name}

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie、`X-CSRF-Token`、`type`、`id`、`name` | DELETE | 不可空 | `image.png` | URL 解码后校验文件名；禁止目录穿越和受保护文件删除。 |
| 响应体 | DELETE | 不可用 | 空 | 成功返回 `204`；不存在返回 `404`。 |

### POST /api/admin/v1/{type}/{id}/assets/download

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie、`X-CSRF-Token`、`type`、`id` | POST | 不可空 | - | 必须通过 Session 和 CSRF。 |
| `url` | POST | 不可空 | `https://example.com/a.png` | 必须是带 Host 的 `http` 或 `https` URL。 |
| `name` | POST | 可为空 | `image.png` | 为空时从远程 URL 推断文件名。 |
| `name`、`path`、`markdown`、`is_image`、`is_cover`、`size` | POST | 不可空 | - | 下载成功返回资源对象和 `201`；失败不得留下半成品。 |

## GET /api/admin/v1/settings

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | GET | 不可空 | `aigoni_session=...` | 必须已登录。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `site` | GET | 不可空 | `{...}` | 站点设置对象。 |
| `site.name`、`description`、`author`、`base_url`、`logo`、`author_avatar`、`utc_offset` | GET | 不可空 | - | Logo 和头像无值时为空字符串。 |
| `pagination` | GET | 不可空 | `{...}` | 分页设置对象。 |
| `pagination.posts_per_page`、`home_posts_count` | GET | 不可空 | `20` | 正整数。 |
| `paths` | GET | 不可空 | `{...}` | 内容和上传目录配置。 |
| `paths.content_dir`、`posts_dir`、`pages_dir`、`notes_dir`、`public_dir`、`uploads_dir` | GET | 不可空 | `"content"` | 安全相对路径，不返回绝对路径。 |
| `uploads` | GET | 不可空 | `{...}` | 上传能力信息。 |
| `uploads.allowed_extensions`、`max_bytes`、`site_asset_path` | GET | 不可空 | - | 允许扩展名、大小和站点资源路径。 |
| `updated_at` | GET | 可为空 | `"2026-08-04T00:00:00Z"` | PATCH 成功时返回，RFC3339。 |
| 密码、API Key、Wiki Key、Session | GET | 不可用 | - | 禁止出现在响应中。 |

## PATCH /api/admin/v1/settings

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie、`X-CSRF-Token` | PATCH | 不可空 | - | 必须通过 Session 和 CSRF。 |
| `site` | PATCH | 可为空 | `{...}` | 省略时保持原值。 |
| `site.name`、`description`、`author`、`base_url`、`logo`、`author_avatar`、`utc_offset` | PATCH | 可为空 | - | 组内字段均可省略；时区必须是 `Z` 或 `±HH:MM`。 |
| `pagination` | PATCH | 可为空 | `{...}` | 省略时保持原值。 |
| `pagination.posts_per_page`、`home_posts_count` | PATCH | 可为空 | `20` | 提供时必须大于 `0`。 |
| `paths` | PATCH | 可为空 | `{...}` | 省略时保持原值。 |
| `paths.content_dir`、`posts_dir`、`pages_dir`、`notes_dir`、`public_dir`、`uploads_dir` | PATCH | 可为空 | `"content"` | 必须是安全非空相对路径，不得包含 `..`。 |
| 请求体 | PATCH | 不可空 | `{...}` | 单个 JSON 对象，最大 `2 MiB`；未知字段和尾随 JSON 值返回 `400`。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `site`、`pagination`、`paths`、`uploads` | PATCH | 不可空 | - | 返回保存并重载后的完整设置。 |
| `updated_at` | PATCH | 不可空 | `"2026-08-04T00:00:00Z"` | 本次保存时间。 |

## PUT /api/admin/v1/settings/assets/{kind}

`{kind}` 只能是 `logo` 或 `avatar`。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie、`X-CSRF-Token` | PUT | 不可空 | - | 必须通过 Session 和 CSRF。 |
| `kind` | PUT | 不可空 | `logo` | 只允许 `logo`、`avatar`。 |
| `asset` | PUT | 不可空 | `logo.png` | `multipart/form-data` 文件字段；最大 `20 MiB`。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `kind`、`name`、`path`、`url` | PUT | 不可空 | - | 上传成功返回 `201`；同类旧资源被替换。 |

## DELETE /api/admin/v1/settings/assets/{kind}

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie、`X-CSRF-Token` | DELETE | 不可空 | - | 必须通过 Session 和 CSRF。 |
| `kind` | DELETE | 不可空 | `logo` | 只允许 `logo`、`avatar`。 |
| 请求体 | DELETE | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `kind` | DELETE | 不可空 | `logo` | 被删除的资源类型。 |
| `deleted` | DELETE | 不可空 | `true` | 是否实际删除文件；重复删除为 `false`。 |

## GET /api/admin/v1/wiki/status

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | GET | 不可空 | `aigoni_session=...` | 必须已登录。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `ready` | GET | 不可空 | `true` | Wiki LLM 是否配置完成。 |

## POST /api/admin/v1/wiki/chat

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie、`X-CSRF-Token` | POST | 不可空 | - | 必须通过 Session 和 CSRF。 |
| `message` | POST | 不可空 | `"同步所有笔记到 Wiki"` | 自然语言任务，去除空白后不可为空。 |
| `history` | POST | 可为空 | `[{"role":"user","content":"上一轮问题"}]` | 浏览器本地历史随请求临时传入；后端不长期保存聊天历史。 |
| 请求体 | POST | 不可空 | `{"message":"检查 Wiki 健康状态","history":[]}` | 单个 JSON 对象，最大 `2 MiB`。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `id`、`kind`、`status` | POST | 不可空 | `"a3f..."`、`"chat"`、`"pending"` | 成功返回 `202` 和 Agent run；Admin Chat 与机器 Ask 共用 `AIGONI_WIKI_AGENT_CONCURRENCY` 总并发池，超过上限返回 `409 run_conflict`。 |
| `message`、`question`、`created_at`、`updated_at` | POST | 可为空 | - | 运行信息；`Location` 指向 `/wiki/runs/{id}`。 |

## POST /api/admin/v1/wiki/chat:stream

### 请求字段

同 `/api/admin/v1/wiki/chat`。

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `step` 事件 | POST | 可为空 | `data: {"title":"开始 Wiki Agent","status":"running"}` | Agent 进度事件。 |
| `tool` 事件 | POST | 可为空 | `data: {"type":"tool","tool":"read","status":"done","args":{"path":"wiki/index.md"},"summary":"读取知识文件","result":{"bytes":128},"started_at":"2026-08-10T09:18:21+08:00","duration_ms":43}` | 工具调用结构化明细；`args` 会脱敏，write 不返回 `content`，read/grep 不返回完整正文，只返回统计摘要、耗时和错误。 |
| `delta` 事件 | POST | 可为空 | `data: {"delta":"...","answer":"..."}` | 增量答案事件；模型重试中的 partial 不会提交。 |
| `done` 事件 | POST | 条件必返 | `data: {...}` | 成功结束事件，含 Markdown、HTML、sources、files。 |
| `error` 事件 | POST | 条件必返 | `data: {"message":"..."}` | 失败事件。 |
| 响应 Content-Type | POST | 不可空 | `text/event-stream; charset=utf-8` | SSE 断开不取消后台 Agent run；后台 run 有 10 分钟超时。 |

## GET /api/admin/v1/wiki/runs/{id}

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | GET | 不可空 | `aigoni_session=...` | 必须已登录。 |
| `id` | GET | 不可空 | `178...` | Agent run ID。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `id`、`kind`、`status` | GET | 不可空 | - | 状态统一为 `pending`、`running`、`completed`、`failed`。 |
| `answer`、`answer_html`、`reasoning`、`sources`、`files` | GET | 可为空 | - | 完成后返回最终结果。 |
| `events` | GET | 不可空 | `[]` | 短期事件缓冲，供刷新后恢复。 |

## POST /api/admin/v1/wiki/runs/{id}/cancel

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie、`X-CSRF-Token` | POST | 不可空 | - | 必须通过 Session 和 CSRF。 |
| `id` | POST | 不可空 | `a3f...` | Agent run ID。 |
| 请求体 | POST | 可为空 | 空 | 不需要 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `id`、`status`、`message`、`error` | POST | 不可空 | - | 活动 run 被取消后返回 `failed`；已结束 run 返回 `409 run_not_active`。 |

## GET /api/admin/v1/wiki/runs/{id}/events

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | GET | 不可空 | `aigoni_session=...` | 必须已登录。 |
| `id` | GET | 不可空 | `178...` | Agent run ID。 |
| `from` | GET | 可为空 | `12` | 从指定事件序号之后继续订阅。 |

### 响应字段

同 `/api/admin/v1/wiki/chat:stream` 的 SSE 事件。

旧 Wiki 更新、问答、保存、Lint 和 Job 接口已移除；调用方统一把自然语言任务提交到 `/api/admin/v1/wiki/chat` 或 `/api/admin/v1/wiki/chat:stream`，再通过 `/api/admin/v1/wiki/runs/{id}` 查询结果。

## POST /api/admin/v1/wiki/ask:api

只读机器接口：用 `AIGONI_API_KEY` 提交自然语言问题，由只读 Wiki Agent 查询 `wiki/**` 与 `content/notes/**`。与浏览器后台 Chat 不同，本接口不要求 Session Cookie 与 CSRF，且 Agent 只提供 `glob`、`grep`、`read` 工具，无法写入、修改或删除 Wiki 文件。

本接口默认关闭，必须通过 `.env` 显式开启：`AIGONI_WIKI_ASK_API_ENABLED=true`。未开启时所有提交请求返回 `403 {"error":"wiki ask api disabled"}`。提交接口受每分钟请求上限约束，由 `AIGONI_WIKI_ASK_API_RPM` 控制（正整数，默认 `60`）；Admin Chat 与机器 Ask 还共用 `AIGONI_WIKI_AGENT_CONCURRENCY` 总并发池（正整数，默认 `5`），`pending` / `running` 都计入。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `X-API-Key` 或 `Authorization: Bearer` | POST | 不可空 | `X-API-Key: <AIGONI_API_KEY>` | 机器鉴权；未配置 `AIGONI_API_KEY` 返回 `503`，Key 错误返回 `401`。 |
| `message` | POST | 不可空 | `"这个 Wiki 里关于某主题的结论是什么？"` | 自然语言问题，去除空白后不可为空。 |
| `history` | POST | 可为空 | `[{"role":"user","content":"上一轮问题"},{"role":"assistant","content":"上一轮回答"}]` | 连续上下文由调用方显式传入；后端不长期保存历史。 |
| 请求体 | POST | 不可空 | `{"message":"...","history":[]}` | 单个 JSON 对象，最大 `2 MiB`。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `id`、`kind`、`status` | POST | 不可空 | `"a3f..."`、`"ask"`、`"pending"` | 成功返回 `202`；`Location` 指向 `/wiki/ask/runs/{id}`。 |
| `message`、`question`、`created_at`、`updated_at` | POST | 可为空 | - | 运行信息。 |

错误响应为机器风格 `{"error":"..."}`：未开启开关返回 `403`，超过每分钟上限返回 `429`，空 `message` 返回 `400`，Wiki LLM 未配置返回 `503`，总并发池已满返回 `409`。

## GET /api/admin/v1/wiki/ask/runs/{id}

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `X-API-Key` 或 `Authorization: Bearer` | GET | 不可空 | `X-API-Key: <AIGONI_API_KEY>` | 机器鉴权。 |
| `id` | GET | 不可空 | `178...` | 只读 ask 的 run ID。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `id`、`kind`、`status` | GET | 不可空 | - | 状态统一为 `pending`、`running`、`completed`、`failed`。 |
| `answer`、`answer_html`、`reasoning`、`sources`、`files` | GET | 可为空 | - | 完成后返回最终结果。 |
| `events` | GET | 不可空 | `[]` | 短期事件缓冲，供轮询恢复。 |
| `error` | GET | 可为空 | - | 失败原因。 |

不存在的 run 或非 `ask` 类型 run 返回 `404 {"error":"wiki ask run not found"}`。查询接口仍受 API Key 保护，不要求 `AIGONI_WIKI_ASK_API_ENABLED` 开关，也不消耗提交接口的每分钟配额或并发额度。只读 ask 结果与浏览器 Chat 一样保存在内存，TTL 30 分钟，服务重启后不可查询；也不提供流式接口，调用方轮询本接口即可。

## GET /api/admin/v1/wiki/documents

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | GET | 不可空 | `aigoni_session=...` | 必须已登录。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `items` | GET | 不可空 | `[]` | Wiki 文档数组。 |
| `items[].path` | GET | 不可空 | `"wiki/index.md"` | 文档相对路径。 |

## GET /api/admin/v1/wiki/documents/content?path={path}

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie | GET | 不可空 | `aigoni_session=...` | 必须已登录。 |
| `path` | GET | 不可空 | `wiki/index.md` | URL 编码后提交；只允许 `wiki/` 和 `content/notes/`。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `path` | GET | 不可空 | `"wiki/index.md"` | 请求的安全相对路径。 |
| `content` | GET | 不可空 | `"# Wiki 索引"` | 去除 frontmatter 后的 Markdown。 |
| `html` | GET | 不可空 | `"<h1>Wiki 索引</h1>"` | 服务端渲染 HTML。 |
| `meta` | GET | 不可空 | `{}` | frontmatter 元信息对象，可为空对象。 |

不存在、路径越界或不允许的文件统一返回 `404`。

## POST /api/admin/v1/wiki/render

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie、`X-CSRF-Token` | POST | 不可空 | - | 必须通过 Session 和 CSRF。 |
| `markdown` | POST | 可为空 | `"## 标题"` | 可省略或为空，按空 Markdown 渲染。 |
| 请求体 | POST | 不可空 | `{"markdown":"## 标题"}` | 单个 JSON 对象，最大 `2 MiB`。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `html` | POST | 不可空 | `"<h2>标题</h2>"` | 渲染后的 HTML。 |

## DELETE /api/admin/v1/wiki/backups

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| Session Cookie、`X-CSRF-Token` | DELETE | 不可空 | - | 必须通过 Session 和 CSRF。 |
| 请求体 | DELETE | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `removed` | DELETE | 不可空 | `3` | 已删除的 Wiki 备份目录数量。 |
| `message` | DELETE | 不可空 | `"已清理 3 个 Wiki 备份目录。"` | 操作提示。 |
