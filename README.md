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

账户在页面内的「管理账户」里添加、编辑和删除（见下文），也可以直接调用管理路由。统计区间与图表、自定义查询、跟随 CPA 代理会在后续版本加入。在那之前，所有账户都直连站点（不使用任何代理，包括环境变量里的代理）。

## 所需权限

- **PAT**：创建时勾选 `profile:read` 和 `usage:read`。不需要 `wallet:read`。
- **旧版访问令牌**：站点个人设置中的系统访问令牌，必须同时填写账户的数字用户 ID。站点升级到新版鉴权后，旧版访问令牌会在过渡期结束时失效，届时页面会提示改用 PAT。

## 添加账户

1. 打开「余额用量 · NewAPI Meter」。还没有账户时，点空状态里的「添加账户」；已有账户时，点账户详情标题行的「管理账户」，再点「添加账户」。
2. 填写表单：
   - **名称**：显示在页签上，用来区分账户。
   - **数据来源**：选「内置 new-api」。
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

校验规则：`name` 必填；`base_url` 只接受 http/https 的绝对地址（不能带用户名密码、query 或 fragment），末尾斜杠会被去掉；`credential` 不以 `nap_` 开头时，`user_id` 必填且必须是数字；`low_balance_threshold` 可省略或为 `null`，否则必须 ≥ 0；`proxy` 为 `follow`（默认）或 `direct`。当前版本只接受 `source: "newapi"`。

列表中的账户：`id`、`order`、`name`、`source`、`base_url`、`credential_masked`、`credential_kind`（`pat` / `legacy`）、`user_id`、`low_balance_threshold`、`proxy`、`custom_script`、`custom_placeholders`（`baseUrl`、`userId`、`apiKeyMasked`、`accessTokenMasked`）。完整凭据从不出现在任何响应或插件日志里。

概况（`overview`）中每个账户的主要字段：`state`（`ok` / `low` / `partial` / `needs_action`）、`currency`（`code` 为 `USD`/`CNY`/`CUSTOM`/`TOKENS`，`symbol`）、`balance`、`total_used`、`today_spend`、`rpm`、`tpm`（读不到时为 `null`）、`low_balance_threshold`、`balance_failure` / `today_failure`（对应数字读不到的原因）、`primary_failure`（需处理时的主要原因）、`failures`（失败的接口）、`fetched_at`、`last_success_at`（CPA 本次运行中最近一次成功读取）、`cached`。顶层还有 `generated_at`、`today_start`（CPA 本机时区的今日零点）和 `timezone`。

测试连接（`accounts/test`）返回：`source`、`ok`（所有计入的查询都成功）、`state`（`ok` / `partial` / `needs_action`，不做余额阈值判断）、`credential_kind`、`site_version`、`currency`、`data_export`、`failure`（最需要处理的失败，成功时为 `null`）、`failures`（失败的接口）。请求体的校验规则与保存相同，但不检查 `name` 和 `low_balance_threshold`；校验失败返回 400 `validation_failed`，`id` 不存在返回 404 `account_not_found`。

失败原因 `code`：`site_unreachable`（网络、DNS、TLS 错误或超时）、`legacy_token_retired`、`pat_scope_denied`、`credential_invalid`、`site_blocked`（站点返回 HTML 而不是 JSON）、`site_error`（5xx）、`bad_response`（其余无法使用的响应，包括 `success: false`）。`diagnostic` 是可以安全展示的诊断代码，例如 `ACCESS_TOKEN_LEGACY_RETIRED`、`HTTP_401`、`HTML_RESPONSE`、`NETWORK_TIMEOUT`。

错误响应形如 `{"error": {"code": "...", "field": "...", "reason": "...", "path": "..."}}`：`validation_failed`（带 `field` 和 `reason`）、`invalid_json`、`account_not_found`，以及数据文件相关的 `data_file_corrupt`、`data_file_newer`、`data_file_unavailable`（带 `path`，状态码 503）。

查询行为：每个账户每种查询的结果在进程内缓存 60 秒；同一账户的并发查询合并成一次；单个站点请求 15 秒超时。

## 许可证

[MIT](LICENSE)
