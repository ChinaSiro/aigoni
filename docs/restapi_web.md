# restapi_web

面向 `frontend/web` 的公开只读 REST API 当前契约。

实现入口：`internal/server/rest_api.go`；路由入口：`internal/server/routes.go`。

## 使用规则

- 基础路径：`/rest/v1`。
- 所有接口均为 `GET`，不接受 JSON 请求体。
- 请求头建议使用 `Accept: application/json`。
- 响应头：`application/json; charset=utf-8`。
- 时间字段使用 RFC3339；存储时间以 UTC 为准。
- 不需要 API Key、Session 或 CSRF。
- 只返回 `publish: true` 的文章和页面；笔记、草稿、未公开内容和后台字段不得返回。
- Query 和路径参数必须 URL 编码。
- 分页默认 `page=1`、`per_page=10`，`per_page` 最大 `100`；非法分页值回退默认值，越界页返回空数组。
- 错误响应格式：`{"error":"错误信息"}`。

## GET /rest/v1

接口索引。`/rest/v1/` 与本 URL 等价。

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `version` | GET | 不可空 | `"v1"` | 当前公开 API 版本。 |
| `endpoints` | GET | 不可空 | `["GET /rest/v1/site"]` | 已注册接口路径数组，可为空数组但不得为 `null`。 |

## GET /rest/v1/site

返回公开站点配置，禁止返回密码、API Key、Session、Wiki 配置、后台路径和服务器绝对路径。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| 请求体 | GET | 不可用 | 空 | GET 不接受 JSON 请求体。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `name` | GET | 不可空 | `"Aigoni"` | 站点名称；可能是空字符串。 |
| `description` | GET | 不可空 | `"公开书架"` | 站点描述；可能是空字符串。 |
| `author` | GET | 不可空 | `"Author"` | 作者名称；可能是空字符串。 |
| `base_url` | GET | 不可空 | `"https://example.com"` | 站点基础地址。 |
| `logo` | GET | 可为空 | `"/uploads/site/logo.png"` | Logo 地址；无 Logo 时为空字符串。 |
| `avatar` | GET | 可为空 | `"/uploads/site/avatar.png"` | 作者头像地址；无头像时为空字符串。 |
| `utc_offset` | GET | 不可空 | `"+08:00"` | 站点显示时区。 |
| `home_posts_count` | GET | 不可空 | `3` | 首页文章数量。 |
| `nav` | GET | 不可空 | `[{"name":"首页","url":"/"}]` | 固定导航数组，可为空数组。 |
| `nav[].name` | GET | 不可空 | `"首页"` | 导航名称。 |
| `nav[].url` | GET | 不可空 | `"/"` | 站内路径。 |
| `features` | GET | 不可空 | `{"categories":true}` | 前台功能开关对象。 |
| `features.categories` | GET | 不可空 | `true` | 分类功能是否可用。 |
| `features.tags` | GET | 不可空 | `true` | 标签功能是否可用。 |
| `features.archives` | GET | 不可空 | `true` | 归档功能是否可用。 |
| `features.search` | GET | 不可空 | `true` | 搜索功能是否可用。 |

## GET /rest/v1/categories

返回公开文章分类及数量；存在公开文章时，固定返回内置“未分类”项。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体或 Query 参数。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `items` | GET | 不可空 | `[]` | 分类数组；没有分类时返回空数组。 |
| `items[].name` | GET | 不可空 | `"Go"` | 分类名称。 |
| `items[].count` | GET | 不可空 | `3` | 公开文章数量。 |
| `items[].url` | GET | 不可空 | `"/category/Go"` | 前台分类路径；“未分类”为 `"/category/__none__"`。 |
| `items[].none` | GET | 可为空 | `true` | 仅“未分类”项返回，表示分类为空。 |
| `total` | GET | 不可空 | `1` | 分类总数，包含“未分类”项。 |

## GET /rest/v1/tags

返回公开文章标签及数量。字段结构与分类接口一致。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体或 Query 参数。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `items` | GET | 不可空 | `[]` | 标签数组；没有标签时返回空数组。 |
| `items[].name` | GET | 不可空 | `"API"` | 标签名称。 |
| `items[].count` | GET | 不可空 | `2` | 公开文章数量。 |
| `items[].url` | GET | 不可空 | `"/tag/API"` | 前台标签路径。 |
| `total` | GET | 不可空 | `1` | 标签总数。 |

## GET /rest/v1/archives

返回公开文章年份归档，年份按倒序排列。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体或 Query 参数。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `items` | GET | 不可空 | `[]` | 归档数组；没有归档时返回空数组。 |
| `items[].year` | GET | 不可空 | `"2026"` | 四位年份。 |
| `items[].count` | GET | 不可空 | `5` | 该年份的公开文章数量。 |
| `items[].url` | GET | 不可空 | `"/archive/2026"` | 前台归档路径。 |
| `total` | GET | 不可空 | `1` | 年份总数。 |

## GET /rest/v1/posts

返回公开文章分页列表。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `page` | GET | 可为空 | `1` | 页码；省略或非法值使用 `1`。 |
| `per_page` | GET | 可为空 | `20` | 每页数量；省略或非法值使用 `10`，最大 `100`。 |
| `category` | GET | 可为空 | `"Go"` | 精确匹配文章分类；传 `__none__` 筛选分类为空的文章。 |
| `tag` | GET | 可为空 | `"API"` | 精确匹配文章标签。 |
| `year` | GET | 可为空 | `"2026"` | 提供时必须是四位数字；非法值返回 `400`。 |
| 请求体 | GET | 不可用 | 空 | 所有筛选通过 Query 提交。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `items` | GET | 不可空 | `[]` | 当前页文章数组，越界页为空数组。 |
| `page` | GET | 不可空 | `1` | 实际使用的页码。 |
| `per_page` | GET | 不可空 | `10` | 实际使用的每页数量。 |
| `total` | GET | 不可空 | `20` | 过滤后的文章总数。 |
| `total_pages` | GET | 不可空 | `2` | 总页数；无数据时为 `0`。 |
| `items[].title` | GET | 不可空 | `"通过 API 发布"` | 文章标题。 |
| `items[].description` | GET | 可为空 | `"文章摘要"` | 摘要；无值时为字符串。 |
| `items[].date` | GET | 不可空 | `"2026-08-04T00:00:00Z"` | RFC3339 发布时间。 |
| `items[].lastmod` | GET | 不可空 | `"2026-08-04T00:00:00Z"` | RFC3339 最后修改时间。 |
| `items[].slug` | GET | 不可空 | `"rest-api"` | 文章 slug。 |
| `items[].category` | GET | 可为空 | `"Go"` | 无分类时省略。 |
| `items[].tags` | GET | 不可空 | `["API"]` | 标签数组，可为空数组。 |
| `items[].cover_image` | GET | 可为空 | `"/assets/posts/cover.png"` | 无封面时省略。 |
| `items[].toc` | GET | 可为空 | `true` | 为 `false` 时可省略。 |
| `items[].url` | GET | 不可空 | `"/post/rest-api"` | 前台文章路径。 |
| `items[].body` | GET | 不可用 | - | 列表不返回正文。 |
| `items[].html` | GET | 不可用 | - | 列表不返回渲染 HTML。 |

## GET /rest/v1/posts/{slug}

返回公开文章详情。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `slug` | GET | 不可空 | `"rest-api"` | 必须是单个非空 URL 路径段；先 URL 解码再查询。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

除列表字段外，额外返回：

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `body` | GET | 可为空 | `"## 标题\n正文"` | 原始 Markdown；为空时可省略。 |
| `html` | GET | 可为空 | `"<h2>标题</h2>"` | 服务端渲染并清理后的 HTML；为空时可省略。 |
| `canonical` | GET | 可为空 | `"https://example.com/post/rest-api"` | 规范 URL；无值时省略。 |
| `previous` | GET | 可为空 | `{"title":"旧文","slug":"old","url":"/post/old"}` | 较旧的相邻文章；没有时省略。 |
| `next` | GET | 可为空 | `{"title":"新文","slug":"new","url":"/post/new"}` | 较新的相邻文章；没有时省略。 |
| `previous.title` / `next.title` | GET | 不可空 | `"相邻文章"` | 相邻文章标题。 |
| `previous.slug` / `next.slug` | GET | 不可空 | `"neighbor"` | 相邻文章 slug。 |
| `previous.url` / `next.url` | GET | 不可空 | `"/post/neighbor"` | 相邻文章前台路径。 |

不存在、未公开或路径不合法时返回 `404`。

## GET /rest/v1/pages

返回公开固定页面分页列表。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `page` | GET | 可为空 | `1` | 省略或非法值使用 `1`。 |
| `per_page` | GET | 可为空 | `20` | 省略或非法值使用 `10`，最大 `100`。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

结构与 `/rest/v1/posts` 相同，但只返回公开页面：

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `items` | GET | 不可空 | `[]` | 当前页页面数组。 |
| `page` | GET | 不可空 | `1` | 实际页码。 |
| `per_page` | GET | 不可空 | `10` | 实际每页数量。 |
| `total` | GET | 不可空 | `1` | 公开页面总数。 |
| `total_pages` | GET | 不可空 | `1` | 总页数。 |
| `items[].title`、`date`、`lastmod`、`slug`、`tags`、`url` | GET | 不可空 | - | 字段含义同文章列表。 |
| `items[].description` | GET | 可为空 | - | 无值时为字符串。 |
| `items[].category`、`cover_image` | GET | 可为空 | - | 为空时可省略。 |
| `items[].toc` | GET | 可为空 | `true` | 为 `false` 时可省略。 |
| `items[].body`、`items[].html` | GET | 不可用 | - | 列表不返回正文和 HTML。 |

## GET /rest/v1/pages/{slug}

返回公开固定页面详情。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `slug` | GET | 不可空 | `"about"` | 必须是单个非空 URL 路径段。 |
| 请求体 | GET | 不可用 | 空 | 不接受 JSON 请求体。 |

### 响应字段

列表字段之外：

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `body` | GET | 可为空 | `"关于本站"` | 原始 Markdown；为空时可省略。 |
| `html` | GET | 可为空 | `"<p>关于本站</p>"` | 服务端渲染并清理后的 HTML；为空时可省略。 |
| `canonical`、`previous`、`next` | GET | 不可用 | - | 页面详情不返回文章规范 URL 或上下篇字段。 |

不存在、未公开或路径不合法时返回 `404`。

## GET /rest/v1/search?q={keyword}

只搜索公开文章，不搜索页面、笔记和草稿。

### 请求字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `q` | GET | 不可空 | `"Markdown"` | 去除空白后不可为空；缺少或为空返回 `400`。 |
| `page` | GET | 可为空 | `1` | 省略或非法值使用 `1`。 |
| `per_page` | GET | 可为空 | `20` | 省略或非法值使用 `10`，最大 `100`。 |
| 请求体 | GET | 不可用 | 空 | 所有条件通过 Query 提交。 |

### 响应字段

| 字段名 | 请求方式 | 是否可空 | 实例 | 备注 |
|---|---|---|---|---|
| `items`、`page`、`per_page`、`total`、`total_pages` | GET | 不可空 | - | 分页字段，结构同文章列表。 |
| `items[].excerpt` | GET | 可为空 | `"命中 <mark>Markdown</mark> 的摘要"` | 命中摘要；为空时可省略。 |
| `items[].title`、`date`、`slug`、`tags`、`url` | GET | 不可空 | - | 文章基础字段。 |
| `items[].description` | GET | 可为空 | - | 无值时为字符串。 |
| `items[].category`、`cover_image`、`toc` | GET | 可为空 | - | 为空时可省略。 |

## 状态码

- `200`：读取成功。
- `400`：年份、搜索参数或请求条件不合法。
- `404`：slug 不存在、内容未公开或路径不合法。
- `500`：服务端读取或搜索失败。

## 变更约束

1. 不得向公开 API 增加写入、Session、API Key 或后台权限。
2. 不得让笔记、草稿和未公开内容通过任何公开接口泄露。
3. 已有字段不得改变类型、含义或空值语义；新增字段必须保持向后兼容。
4. 新增公开接口必须同时补充 `internal/server/rest_api_test.go` 和本文件对应 URL 的字段表。
5. API 路由必须优先于 SPA fallback，错误必须返回 JSON，不能返回 `frontend/web/index.html`。
