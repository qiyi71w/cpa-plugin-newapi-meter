# NewAPI Meter

CPA（CLIProxyAPI）原生插件。在 CPA WebUI 或 EasyCLIProxyAPI 的插件菜单里打开「余额用量 · NewAPI Meter」，就能在一处看到你在多个 new-api 系站点上的账户：每个账户的状态、余额、今日花费，以及余额相对你设定的余额阈值还剩多少。

- 读取的是**账户级**数据（余额、累计已用、今日花费、实时 RPM/TPM），口径与站点后台一致；按 API Key 查额度请用 balance-query 一类插件。
- 账户凭据只保存在插件自己的数据文件里，不写进 CPA 配置；页面和管理接口只返回打码值。
- 插件只在你打开页面或点「刷新」时访问站点，不在后台拉取。
- 页面跟随宿主主题（CPA WebUI 的暖灰 / 纯白 / 深色，或系统浅色 / 深色）和系统语言（中文 / 英文）。

术语见 [GLOSSARY.md](GLOSSARY.md)，产品约束见 [PRODUCT.md](PRODUCT.md)，设计决策见 [docs/adr/](docs/adr/)。

## 当前能力

- 内置 new-api 数据来源：PAT（`nap_` 开头）只发 `Authorization: Bearer`；旧版访问令牌另外发 `New-Api-User: <用户 ID>`。
- 金额按站点的显示货币换算（美元、人民币、自定义货币或 token 数）；所有能计入的账户显示货币一致时，页签带最左侧显示「全部账户合计」。
- 账户状态：正常、余额偏低、部分可用（部分数据读不到，例如余额被站点防护拦截）、需处理（凭据相关的查询全部失败），并给出失败原因和下一步。
- 今日按 **CPA 所在机器的本地时区**从零点算起。
- 自定义查询数据来源：粘贴兼容 cc-switch 格式的用量脚本，接入非 new-api 服务或只有 API Key 的情况，账户和内置账户出现在同一排页签里（见下文「自定义查询」）。

账户在页面内的「管理账户」里添加、编辑和删除（见下文），也可以直接调用管理路由。统计区间与图表、跟随 CPA 代理会在后续版本加入。在那之前，所有账户都直连站点（不使用任何代理，包括环境变量里的代理）。

## 所需权限

- **PAT**：创建时勾选 `profile:read` 和 `usage:read`。不需要 `wallet:read`。
- **旧版访问令牌**：站点个人设置中的系统访问令牌，必须同时填写账户的数字用户 ID。站点升级到新版鉴权后，旧版访问令牌会在过渡期结束时失效，届时页面会提示改用 PAT。

## 添加账户

1. 打开「余额用量 · NewAPI Meter」。还没有账户时，点空状态里的「添加账户」；已有账户时，点账户详情标题行的「管理账户」，再点「添加账户」。
2. 填写表单：
   - **名称**：显示在页签上，用来区分账户。
   - **数据来源**：选「内置 new-api」。非 new-api 服务选「自定义查询」，表单换成脚本和占位符，见下文「自定义查询」。
   - **站点地址**：站点首页的地址，例如 `https://newapi.example.com`。只支持 http 和 https，末尾的斜杠会被去掉。
   - **账户凭据**：PAT（`nap_` 开头）或旧版访问令牌。页面会提示识别出的类型。
   - **用户 ID**：只在填旧版访问令牌时出现，必填，是站点个人设置里显示的数字 ID。
   - **余额阈值**（可选）：余额低于它时，账户显示为余额偏低。单位是这个账户的显示货币，测试连接后会显示出来。
   - **代理**：「跟随 CPA 代理」或「直连」。
3. 点「测试连接」。插件用表单里还没保存的值向站点查询一次，显示识别出的凭据类型、站点版本和显示货币，或者具体的失败原因和下一步。测试不会保存任何东西。
4. 点「添加账户」保存。页面回到仪表盘，新账户出现在页签带里。

编辑账户时，凭据只显示打码值；凭据框留空表示继续使用已保存的凭据，测试连接也会用它。需处理的账户在详情里有「替换凭据」，直接打开这个账户的编辑表单。删除账户前会弹出写明账户名的确认；删除只影响插件的数据文件，不影响站点上的账户。

### 测试连接的失败说明

| 结果 | 含义 | 下一步 |
|---|---|---|
| 站点无法访问 | 网络、DNS、TLS 错误或超时 | 检查站点地址和网络连接，然后重试 |
| 旧版访问令牌已失效 | 站点已升级到新版鉴权，旧版访问令牌过了过渡期 | 在站点个人设置里创建 PAT（勾选 `profile:read` 和 `usage:read`），替换凭据 |
| PAT 权限不足 | PAT 缺少 `profile:read` 或 `usage:read` | 重新创建勾选这两项的 PAT，替换凭据 |
| 凭据无效 | 站点拒绝了凭据（401 等） | 确认凭据和用户 ID 无误，或在站点重新生成后替换 |
| 站点防护拦截 | 站点返回了网页验证（HTML），而不是数据 | 插件不会绕过站点防护；请站点放行这些接口，或稍后重试 |
| 站点出错 | 站点返回 5xx | 稍后重试 |
| 响应异常 | 返回了无法识别的数据 | 确认这是受支持的 new-api 站点后重试 |

如果只是部分接口失败（例如余额接口被站点防护拦截，花费正常），测试结果会列出读不到的部分；这样的账户保存后显示为部分可用。站点没有开启数据看板时测试仍算成功，结果里会注明保存后看不到按模型用量。

## 自定义查询

非 new-api 服务，或者只有 API Key 的情况，用「自定义查询」数据来源接入：粘贴一段用量脚本，插件按脚本发出请求、提取余额和套餐。脚本格式兼容 [cc-switch](https://github.com/farion1231/cc-switch) 的用量脚本，它的「通用」和「New API」模板可以原样使用。

### 脚本格式

脚本求值得到一个对象：`request` 描述要发的请求，`extractor` 把解析后的 JSON 响应转成结果。

```js
({
  request: {
    url: "{{baseUrl}}/user/balance",          // 必填，只允许 http 和 https
    method: "GET",                            // 可省略，默认 GET
    headers: { "Authorization": "Bearer {{apiKey}}" },
    body: undefined                           // 可选，字符串
  },
  extractor: function (response) {            // response 是解析后的 JSON
    return { remaining: response.balance, unit: "USD" };
  }
})
```

`extractor` 返回一个对象，或者对象数组（每项是一个套餐）。字段都可省略，`null` 视同省略：

| 字段 | 类型 | 含义 |
|---|---|---|
| `remaining` | 数字 | 余额（剩余） |
| `used` | 数字 | 累计已用 |
| `total` | 数字 | 总额度 |
| `unit` | 字符串 | 显示货币：`USD`、`CNY` 按美元、人民币显示，其他单位原样显示在数字后面 |
| `planName` | 字符串 | 套餐名 |
| `extra` | 字符串 | 套餐的附加说明，显示在套餐名下面 |
| `isValid` | 布尔 | `false` 时账户进入「需处理」 |
| `invalidMessage` | 字符串 | `isValid: false` 时显示的原因 |
| `todaySpend` | 数字 | **本插件的扩展**：今日花费，单位同 `unit` |

结果怎么用：

- 只有一项时，余额、累计已用、总额度就是这一项的值。
- 有多项且 `unit` 全部相同时，余额是各项 `remaining` 之和，页签和详情标「合计」；`unit` 不同时取第一项，标「第一项」。累计已用和总额度按同样的规则取。
- 今日花费取第一个提供 `todaySpend` 的项。
- 任何一项 `isValid` 为 `false` 时，账户进入「需处理」，显示第一个这样的项的 `invalidMessage`。
- 自定义查询账户同样参与余额阈值判断，也计入「全部账户合计」：合计按单位字符串判断是否一致，所以 `unit: "USD"` 的账户和美元站点的内置账户可以合计。
- 自定义查询账户没有统计区间和图表；详情显示余额、累计已用、总额度、今日花费和套餐表。

### 占位符

运行前，插件在脚本文本里替换下面的占位符。只替换一遍，替换进去的值不会再被当作占位符；字符串值会转义，放在单引号、双引号或模板字符串里都不会破坏脚本。

| 占位符 | 取值 |
|---|---|
| `{{baseUrl}}` | 表单里的 baseUrl（可选；只允许 http/https，末尾斜杠会被去掉；账户列表显示它的主机名） |
| `{{apiKey}}` | 表单里的 apiKey |
| `{{accessToken}}` | 表单里的 accessToken |
| `{{userId}}` | 表单里的 userId |
| `{{todayStart}}` | **扩展**：CPA 本机时区今天零点的 Unix 秒（数字，不带引号） |
| `{{now}}` | **扩展**：当前时刻的 Unix 秒（数字，不带引号） |

apiKey 和 accessToken 与账户凭据同样处理：保存后只显示打码值，编辑时留空表示继续使用已保存的值。baseUrl、userId 和脚本原文可以直接查看和修改。所有占位符的取值都不会写进插件日志。

### 添加和测试

在「管理账户」里添加账户，数据来源选「自定义查询」，粘贴脚本，填写用到的占位符取值，可选填余额阈值和代理选项。点「测试脚本」，插件用表单里还没保存的值运行一次，显示提取出的余额、累计已用、总额度、今日花费和套餐，或者脚本报错；测试不会保存任何东西。返回给页面的报错里，替换进去的 apiKey 和 accessToken 会被遮掉。

测试脚本和账户状态可能出现的失败：

| 结果 | 含义 |
|---|---|
| 脚本出错（`script_error`） | 语法错误、运行时异常、`request` 不合法（例如不是 http/https 地址），或者 `extractor` 的返回值不符合上面的格式；页面显示引擎给出的报错 |
| 查询超时（`query_timeout`） | 请求加脚本超过 10 秒，脚本被中断 |
| 脚本报告账户不可用（`reported_invalid`） | `extractor` 返回了 `isValid: false` |
| 响应异常（`bad_response`） | 2xx 响应不是 JSON，或者响应超过 1 MiB |
| 站点无法访问 / 凭据无效 / 站点出错 等 | 请求失败或返回非 2xx，分类与内置来源相同 |

### 限制与风险

- 脚本运行在插件内嵌的纯 Go JavaScript 引擎里，只有 ECMAScript 内置对象：没有 `fetch`、`XMLHttpRequest`、`require`、`import`、`console`、定时器，也不能读写文件。脚本唯一能发出的请求是 `request` 描述的那一个，由插件代发。
- 每次查询（请求加脚本执行）总时限 **10 秒**，超时会中断正在运行的脚本；响应最大 **1 MiB**。
- **脚本运行在 CPA 进程内，内存占用无法限制。** 一个故意或者写错的脚本可以在 10 秒内占用大量内存，影响 CPA 本身。只粘贴你信任的脚本；只有持有 Management Key 的人能保存脚本。
- 请求和内置来源走同一个出站入口，代理选项的效果相同。
- 和账户凭据一样，apiKey、accessToken 明文保存在插件的数据文件里（见「数据目录与备份」）。

## 从源码构建

需要 Go 1.25+、可用的 C 编译器（cgo）和 [bun](https://bun.sh)。

```bash
bun run typecheck                 # 前端类型检查
bun run test:web                  # 前端单元测试（Management Key 的三种存储格式、账户表单校验）
go test ./...                     # Go 测试（合成的假站点，不访问网络）
bun run build                     # 先构建页面，再构建插件
python3 tests/native_abi.py dist/linux/amd64/newapi-meter.so   # 加载真实产物的 C ABI 冒烟
```

- `bun run build:web` 只构建页面：`internal/webui/dist/index.html`，一个 JS/CSS 全部内联的 HTML，随后被嵌入动态库。修改前端后要先构建页面，再构建插件。
- `bun run build` 输出 `dist/<goos>/<goarch>/newapi-meter.{so,dll,dylib}`。环境变量：`GOOS`/`GOARCH` 选择目标（默认本机），`VERSION` 设定插件版本（默认 `0.0.0-dev`），`GO` 指定 go 可执行文件。交叉编译 cgo 需要对应平台的 `CC`。
- 插件 ID 取自文件名，必须保持 `newapi-meter`。

手动安装：把产物放到 CPA 插件目录的同名平台子目录（例如 `plugins/linux/amd64/newapi-meter.so`），在 CPA 配置中启用：

```yaml
plugins:
  enabled: true
  configs:
    newapi-meter:
      enabled: true
      # data_dir: /absolute/path/to/data   # 可选，见下文
```

## 数据目录与备份

- 账户和凭据保存在数据目录下的 `accounts.json`。默认数据目录是 CPA 运行用户的配置目录下的 `cpa-plugin-newapi-meter`：Linux 为 `~/.config/cpa-plugin-newapi-meter`，Windows 为 `%APPDATA%\cpa-plugin-newapi-meter`，macOS 为 `~/Library/Application Support/cpa-plugin-newapi-meter`。
- 可以用插件配置项 `data_dir`（绝对路径）改到别处；插件会在目录不存在时创建它。请给它一个插件专用的目录。
- Unix 上新建的目录权限为 0700，数据文件为 0600。文件没有额外加密，任何能读这个文件的人都能拿到账户凭据。
- 写入使用同目录临时文件加原子替换。如果数据文件无法解析，或者是更新版本的插件写的（`schema_version` 更高），插件**拒绝覆盖**，页面显示文件位置和原因；修复或从备份恢复后刷新即可，不用重启 CPA。
- 没有导入导出。备份就是复制 `accounts.json`；恢复就是把它放回去。用 Docker 运行 CPA 时，把数据目录挂载到卷上，否则重建容器会丢失账户。
- 卸载插件不会删除数据目录。

## 管理路由（开发者说明）

页面的所有数据都来自插件注册的管理路由，挂在 CPA 管理前缀下，需要 Management Key（`Authorization: Bearer <key>` 或 `X-Management-Key: <key>`）。路由按「方法 + 精确路径」匹配，参数用 query 或 JSON body 传。资源路由不鉴权，只返回静态页面。

| 方法 | 路径 | 参数 | 说明 |
|---|---|---|---|
| GET | `/v0/resource/plugins/newapi-meter/index.html` | — | 页面（宿主菜单「余额用量 · NewAPI Meter」）；不含任何数据 |
| GET | `/v0/management/plugins/newapi-meter/accounts` | — | 账户列表，凭据只给打码值 |
| POST | `/v0/management/plugins/newapi-meter/accounts` | JSON 账户 | 新建账户，返回 201 |
| PUT | `/v0/management/plugins/newapi-meter/accounts` | JSON 账户 + `id` | 更新账户：除凭据外的字段整体替换；凭据（以及 `apiKey`、`accessToken`）留空或原样传回打码值表示保留原值 |
| DELETE | `/v0/management/plugins/newapi-meter/accounts` | query `id` | 删除账户 |
| GET | `/v0/management/plugins/newapi-meter/overview` | query `force=1`，可选 `id` | 全部账户的页签数据；`force=1` 跳过缓存，同时带 `id` 时只有该账户跳过缓存 |
| POST | `/v0/management/plugins/newapi-meter/accounts/test` | JSON 账户，可选 `id` | 测试连接：用尚未保存的值向站点查询一次，不缓存、不写数据文件；带 `id` 时凭据留空或传回打码值表示用该账户已保存的凭据 |

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

`custom_script` 必填；`custom_placeholders.baseUrl` 可选，填写时规则与 `base_url` 相同，保存后同时作为账户的 `base_url`。`apiKey`、`accessToken` 留空或原样传回打码值表示保留原值。

列表中的账户：`id`、`order`、`name`、`source`、`base_url`、`credential_masked`、`credential_kind`（`pat` / `legacy`）、`user_id`、`low_balance_threshold`、`proxy`、`custom_script`、`custom_placeholders`（`baseUrl`、`userId`、`apiKeyMasked`、`accessTokenMasked`）。完整凭据从不出现在任何响应或插件日志里。

概况（`overview`）中每个账户的主要字段：`state`（`ok` / `low` / `partial` / `needs_action`）、`currency`（`code` 为 `USD`/`CNY`/`CUSTOM`/`TOKENS`，自定义查询的其他单位为 `UNIT`，`symbol` 是单位原文）、`balance`、`total_used`、`today_spend`、`rpm`、`tpm`（读不到时为 `null`）、`low_balance_threshold`、`balance_failure` / `today_failure`（对应数字读不到的原因）、`primary_failure`（需处理时的主要原因）、`failures`（失败的接口）、`fetched_at`、`last_success_at`（CPA 本次运行中最近一次成功读取）、`cached`。顶层还有 `generated_at`、`today_start`（CPA 本机时区的今日零点）和 `timezone`。

自定义查询账户另有 `custom`：`balance_basis`（`sum` 合计 / `first` 第一项；只有一项时省略）、`total_quota`、`plans`（每项 `plan_name`、`extra`、`remaining`、`used`、`total`、`today_spend`、`unit`、`currency`、`is_valid`、`invalid_message`）、`invalid_message`（脚本报告不可用时）、`script_error`（脚本出错时的报错，已遮掉 apiKey 和 accessToken）。

测试连接（`accounts/test`）返回：`source`、`ok`（所有计入的查询都成功）、`state`（`ok` / `partial` / `needs_action`，不做余额阈值判断）、`credential_kind`、`site_version`、`currency`、`data_export`、`failure`（最需要处理的失败，成功时为 `null`）、`failures`（失败的接口）。自定义查询账户的测试（页面上的「测试脚本」）另返回 `custom`，字段同概况里的 `custom`，再加上 `balance`、`total_used`、`today_spend`。请求体的校验规则与保存相同，但不检查 `name` 和 `low_balance_threshold`；校验失败返回 400 `validation_failed`，`id` 不存在返回 404 `account_not_found`。

失败原因 `code`：`site_unreachable`（网络、DNS、TLS 错误或超时）、`legacy_token_retired`、`pat_scope_denied`、`credential_invalid`、`site_blocked`（站点返回 HTML 而不是 JSON）、`site_error`（5xx）、`bad_response`（其余无法使用的响应，包括 `success: false`），以及自定义查询的 `script_error`、`query_timeout`、`reported_invalid`。`diagnostic` 是可以安全展示的诊断代码，例如 `ACCESS_TOKEN_LEGACY_RETIRED`、`HTTP_401`、`HTML_RESPONSE`、`NETWORK_TIMEOUT`；自定义查询另有 `SCRIPT_SYNTAX`、`SCRIPT_RUNTIME`、`SCRIPT_REQUEST`、`REQUEST_URL`、`REQUEST_SCHEME`、`REQUEST_METHOD`、`REQUEST_HEADER`、`SCRIPT_RESULT`、`SCRIPT_TIMEOUT`、`REQUEST_TIMEOUT`、`IS_VALID_FALSE`、`INVALID_JSON`、`RESPONSE_TOO_LARGE`。

错误响应形如 `{"error": {"code": "...", "field": "...", "reason": "...", "path": "..."}}`：`validation_failed`（带 `field` 和 `reason`）、`invalid_json`、`account_not_found`，以及数据文件相关的 `data_file_corrupt`、`data_file_newer`、`data_file_unavailable`（带 `path`，状态码 503）。

查询行为：每个账户每种查询的结果在进程内缓存 60 秒；同一账户的并发查询合并成一次；单个站点请求 15 秒超时；自定义查询整体（请求加脚本）10 秒超时。

## 许可证

[MIT](LICENSE)
