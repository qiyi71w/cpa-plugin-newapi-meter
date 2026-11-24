# 管理路由

[返回 README](../README.md)

页面的所有数据都来自插件注册的管理路由，挂在 CPA 管理前缀下，需要 Management Key（`Authorization: Bearer <key>` 或 `X-Management-Key: <key>`）。路由按「方法 + 精确路径」匹配，参数用 query 或 JSON body 传。资源路由不鉴权，只返回静态页面。

## 路由一览

| 方法 | 路径 | 参数 | 说明 |
|---|---|---|---|
| GET | `/v0/resource/plugins/newapi-meter/index.html` | — | 页面（宿主菜单「NewAPI 余额用量」）；不含任何数据 |
| GET | `/v0/management/plugins/newapi-meter/accounts` | — | 账户列表，凭据只给打码值 |
| POST | `/v0/management/plugins/newapi-meter/accounts` | JSON 账户 | 新建账户，返回 201 |
| PUT | `/v0/management/plugins/newapi-meter/accounts` | JSON 账户 + `id` | 更新账户：除凭据外的字段整体替换；凭据（以及 `apiKey`、`accessToken`）留空或原样传回打码值表示保留原值 |
| DELETE | `/v0/management/plugins/newapi-meter/accounts` | query `id` | 删除账户 |
| GET | `/v0/management/plugins/newapi-meter/overview` | query `force=1`，可选 `id` | 全部账户的页签数据；`force=1` 跳过缓存，同时带 `id` 时只有该账户跳过缓存 |
| POST | `/v0/management/plugins/newapi-meter/accounts/test` | JSON 账户，可选 `id` | 测试连接：用尚未保存的值向站点查询一次，不缓存、不写数据文件；带 `id` 时凭据留空或传回打码值表示用该账户已保存的凭据 |
| GET | `/v0/management/plugins/newapi-meter/detail` | query `id`，`period`，自定义时 `start`/`end`，可选 `force=1` | 单个内置账户在统计区间内的花费与用量，见下文 |

## 账户字段

新建 / 更新的 JSON 字段：

```json
{
  "name": "主力账户",
  "source": "newapi",
  "base_url": "https://newapi.example.com",
  "credential": "nap_xxxxxxxx",
  "user_id": "",
  "low_balance_threshold": 10,
  "proxy": "follow"
}
```

校验规则：`name` 必填；`base_url` 只接受 http/https 的绝对地址（不能带用户名密码、query 或 fragment），末尾斜杠会被去掉；`credential` 不以 `nap_` 开头时，`user_id` 必填且必须是数字；`low_balance_threshold` 可省略或为 `null`，否则必须 ≥ 0；`proxy` 为 `follow`（默认）或 `direct`。`source` 为 `newapi` 或 `custom`。

自定义查询账户（`source: "custom"`）不传 `base_url`、`credential`、`user_id`，改传：

```json
{
  "name": "某服务",
  "source": "custom",
  "custom_script": "({ request: { url: \"{{baseUrl}}/user/balance\" }, extractor: function (r) { return { remaining: r.balance, unit: \"USD\" }; } })",
  "custom_placeholders": {
    "baseUrl": "https://api.example.com",
    "apiKey": "sk-xxxxxxxx",
    "accessToken": "",
    "userId": ""
  },
  "low_balance_threshold": 5,
  "proxy": "follow"
}
```

`custom_script` 必填；`custom_placeholders.baseUrl` 可选，填写时规则与 `base_url` 相同，保存后同时作为账户的 `base_url`。`apiKey`、`accessToken` 留空或原样传回打码值表示保留原值。切换数据来源时，另一种来源的密钥不保留：保存为自定义查询会清除内置来源的 `credential` 和 `user_id`，保存为内置来源会清除 `apiKey` 和 `accessToken`，之后再切回自定义查询需要重新填写。

## 账户列表

`GET /accounts` 返回 `{"accounts": […], "data_file": "<数据文件路径>"}`：`data_file` 是数据文件 `accounts.json` 的绝对路径（管理视图在列表下显示它，方便备份）。列表中的账户：`id`、`order`、`name`、`source`、`base_url`、`credential_masked`、`credential_kind`（`pat` / `legacy`）、`user_id`、`low_balance_threshold`、`proxy`、`custom_script`、`custom_placeholders`（`baseUrl`、`userId`、`apiKeyMasked`、`accessTokenMasked`）。完整凭据从不出现在任何响应或插件日志里。

## 概况

概况（`overview`）中每个账户的主要字段：`state`（`ok` / `low` / `partial` / `needs_action`）、`currency`（`code` 为 `USD`/`CNY`/`CUSTOM`/`TOKENS`，自定义查询的其他单位为 `UNIT`，`symbol` 是单位原文）、`balance`、`total_used`、`today_spend`、`rpm`、`tpm`（读不到时为 `null`）、`today_spend_currency`（`today_spend` 的单位，形状同 `currency`；`today_spend` 为 `null` 时也为 `null`；内置账户等于 `currency`，自定义查询账户是提供今日花费的那一项的单位）、`low_balance_threshold`、`balance_failure` / `today_failure`（对应数字读不到的原因）、`primary_failure`（需处理时的主要原因）、`failures`（失败的接口）、`fetched_at`、`last_success_at`（CPA 本次运行中最近一次成功读取）、`cached`。顶层还有 `generated_at`、`today_start`（CPA 本机时区的今日零点）和 `timezone`。

自定义查询账户另有 `custom`：`balance_basis`（`sum` 合计 / `first` 第一项；只有一项时省略）、`total_quota`、`plans`（每项 `plan_name`、`extra`、`remaining`、`used`、`total`、`today_spend`、`unit`、`currency`、`is_valid`、`invalid_message`）、`invalid_message`（脚本报告不可用时）、`script_error`（引擎或脚本给出的报错原文，已遮掉 apiKey 和 accessToken）、`script_reason`（插件自己判断出的脚本问题，代替 `script_error`：`code` 是稳定的原因代码，如 `url_scheme`、`header_name_invalid`、`field_not_number`；`field` 是涉及的字段，如 `result[1].remaining`，可省略）。

## 测试连接

测试连接（`accounts/test`）返回：`source`、`ok`（所有计入的查询都成功）、`state`（`ok` / `partial` / `needs_action`，不做余额阈值判断）、`credential_kind`、`site_version`、`currency`、`data_export`、`failure`（最需要处理的失败，成功时为 `null`）、`failures`（失败的接口）。自定义查询账户的测试（页面上的「测试脚本」）另返回 `custom`，字段同概况里的 `custom`，再加上 `balance`、`total_used`、`today_spend` 和 `today_spend_currency`。请求体的校验规则与保存相同，但不检查 `name` 和 `low_balance_threshold`；校验失败返回 400 `validation_failed`，`id` 不存在返回 404 `account_not_found`。

## 账户详情

账户详情（`detail`）的参数：`period` 为 `today`（默认）、`yesterday`、`7d`、`30d` 或 `custom`；`custom` 时 `start` 和 `end` 是 CPA 本机时区的日期 `YYYY-MM-DD`（含首尾两天）。不合法时返回 400 `validation_failed`，`field` 为 `period` / `start` / `end`，`reason` 除通用的 `required`、`invalid`、`unsupported` 外还有 `swapped`（结束早于开始）、`too_long`（超过 365 天）、`future`（晚于今天）；自定义查询账户返回 `field: "id"`、`reason: "unsupported"`。今日区间（包括起止都是今天的自定义区间）直接复用概况的今日查询和缓存，不再访问站点；其他区间各自缓存 60 秒，`force=1` 跳过缓存。响应字段：

- `period`：`preset`、`start_date`、`end_date`、`days`、`start` / `end`（RFC 3339，带 CPA 本机时区偏移；`end` 是最后一天之后的零点）、`granularity`（`hour` / `day`）、`includes_now`、`minutes`（平均 RPM/TPM 的分母）；顶层还有 `timezone`。
- `currency`、`data_export`、`spend`（区间花费，来自 `/api/log/self/stat`）及 `spend_failure`。
- `usage`：`spend`、`requests`、`tokens`、`avg_rpm`、`avg_tpm`、`models`（按花费从高到低，每项 `model`、`spend`、`share`、`requests`、`tokens`）、`buckets`（每个小时或每天一项：`start`，以及与 `models` 顺序对应的 `spend`、`requests`、`tokens` 数组）；读不到时为 `null`，原因在 `usage_failure`。
- `usage_disabled`：站点未开启数据看板，此时 `usage` 和 `usage_failure` 都为 `null`。
- `fetched_at`、`cached`。区间查询失败只出现在这些字段里，不影响概况中的账户状态。

## 失败与错误

失败原因 `code`：`site_unreachable`（网络、DNS、TLS 错误或超时）、`legacy_token_retired`、`pat_scope_denied`、`credential_invalid`、`site_blocked`（站点返回 HTML 而不是 JSON）、`site_error`（5xx）、`bad_response`（其余无法使用的响应，包括 `success: false`），以及自定义查询的 `script_error`、`query_timeout`、`reported_invalid`。`diagnostic` 是可以安全展示的诊断代码，例如 `ACCESS_TOKEN_LEGACY_RETIRED`、`HTTP_401`、`HTML_RESPONSE`、`NETWORK_TIMEOUT`；自定义查询另有 `SCRIPT_SYNTAX`、`SCRIPT_RUNTIME`、`SCRIPT_REQUEST`、`REQUEST_URL`、`REQUEST_SCHEME`、`REQUEST_METHOD`、`REQUEST_HEADER`、`SCRIPT_RESULT`、`SCRIPT_TIMEOUT`、`REQUEST_TIMEOUT`、`PREVIOUS_QUERY_RUNNING`、`IS_VALID_FALSE`、`INVALID_JSON`、`RESPONSE_TOO_LARGE`。

错误响应形如 `{"error": {"code": "...", "field": "...", "reason": "...", "path": "..."}}`：`validation_failed`（带 `field` 和 `reason`）、`invalid_json`、`account_not_found`，以及数据文件相关的 `data_file_corrupt`、`data_file_newer`、`data_file_unavailable`（带 `path`，状态码 503）。`data_file_unavailable` 在目录或文件对其他用户开放时带 `reason`：`dir_not_private`（配置的数据目录给同组或其他用户留了权限，`path` 是该目录）或 `file_not_private`（数据文件无法收紧为 0600，`path` 是该文件）。另有三个与具体账户无关的错误：`invalid_request`（400，宿主转来的管理请求无法解析）、`method_not_allowed`（405，用 GET 以外的方法请求资源路由的页面）、`not_found`（404，没有与「方法 + 路径」匹配的路由）。

## 查询行为

每个账户每种查询的结果在进程内缓存 60 秒，今日的查询（概况和自定义查询）在 CPA 本机时区跨过零点后不再沿用前一天的结果；同一账户的并发查询合并成一次；单个站点请求 15 秒超时；自定义查询的整个查询有 10 秒时限（请求、脚本运行和插件处理脚本结果都算在内）。
