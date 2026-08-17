# 机器写入 API

通过 `AIGONI_API_KEY` 鉴权，用一个接口创建笔记、文章或独立页面。内容元信息默认写在 Markdown 的 YAML frontmatter 中，JSON 请求体包含类型、完整 Markdown、图片同步开关，以及仅限文章/页面的发布开关。

## 通用约定

- Content-Type：`application/json`
- 鉴权头二选一：
  - `X-API-Key: <AIGONI_API_KEY>`
  - `Authorization: Bearer <AIGONI_API_KEY>`
- 单次 JSON 请求体上限：2 MiB
- `sync_images` 默认 `false`。设为 `true` 时，下载正文中的 Markdown 网络图片 `![描述](http/https...)` 到当前内容的同名 `.assets` 目录，并替换正文 URL。
- 单张网络图片上限：20 MiB。任意图片同步失败时，本次新建内容和已下载资源会一起删除。

## 新增内容

- URL：`POST /api/content`

请求字段：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| type | string | 是 | `note` / `post` / `page` |
| body | string | 是 | 完整 Markdown 文档，包含 YAML frontmatter 与正文 |
| sync_images | boolean | 否 | 是否同步正文中的网络图片，默认 false |
| publish | boolean | 否 | 仅 `post` / `page` 支持；传入时覆盖 frontmatter 的 `publish`，`note` 不接受 |

元信息不再作为 JSON 字段，统一写在 `body` 的 YAML frontmatter；唯一例外是 `publish` 可在 JSON 层控制 `post` / `page` 是否发布：

- `note`：支持 `title`、`description`、`date`、`category`、`source_url`、`tags`；`date` 必填，`title` 可为空。
- `post` / `page`：支持 `title`、`description`、`date`、`slug`、`publish`、`category`、`tags`、`toc` 等；`title`、`date`、`slug` 必填；`publish` 可写在 frontmatter，也可由 JSON `publish` 提供。
- `date` / `lastmod` 支持 `2026-08-10`、`2026-08-10 13:20`、`2026-08-10 13:20:30`、`2026-08-10T13:20:00Z`；落盘和响应统一为 RFC3339。

### 笔记示例

```bash
curl -X POST https://example.com/api/content \
  -H "Content-Type: application/json" \
  -H "X-API-Key: sk-123" \
  -d '{
    "type": "note",
    "body": "---
title: 一句话笔记
date: 2026-08-10
tags: [灵感]
---

![示例](https://example.com/image.png)

随手记下的内容",
    "sync_images": true
  }'
```

### 文章示例

```bash
curl -X POST https://example.com/api/content \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-123" \
  -d '{
    "type": "post",
    "body": "---
title: 通过 API 发布
slug: publish-by-api
date: 2026-08-10 13:20
publish: true
toc: true
---

## 第一节

正文",
    "sync_images": true,
    "publish": true
  }'
```

### 独立页面示例

`type` 改为 `page`，其余规则与文章一致。

## 响应

`201 Created`：

```json
{
  "id": "2026/2026-08-10-1",
  "path": "2026/2026-08-10-1.md",
  "title": "通过 API 发布",
  "date": "2026-08-10T00:00:00Z",
  "slug": "publish-by-api",
  "publish": true,
  "toc": true,
  "synced_images": 1
}
```

`post` / `page` 响应包含 `slug`、`publish`、`toc`；笔记不返回这三个字段。`synced_images` 表示成功同步的唯一网络图片数量。

错误状态：

- `400`：JSON、`type`、`body` 或 frontmatter 不合法（包括未知 JSON 字段与尾随 JSON 值）。
- `401`：API Key 错误。
- `502`：网络图片下载失败。
- `503`：未配置 `AIGONI_API_KEY`。
