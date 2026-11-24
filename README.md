# NewAPI Meter

CPA（CLIProxyAPI）原生插件。在 CPA WebUI 或 EasyCLIProxyAPI 的插件菜单里打开「NewAPI 余额用量」，就能在一处看到你在多个 new-api 系站点上的账户：每个账户的状态、余额、今日花费，以及余额相对你设定的余额阈值还剩多少。

- 读取的是**账户级**数据（余额、累计已用、今日花费、实时 RPM/TPM），口径与站点后台一致；按 API Key 查额度请用 balance-query 一类插件。
- 账户凭据只保存在插件自己的数据文件里，不写进 CPA 配置；页面和管理接口只返回打码值。
- 插件只在你打开页面或点「刷新」时访问站点，不在后台拉取。
- 页面跟随宿主主题（CPA WebUI 的暖灰 / 纯白 / 深色，或系统浅色 / 深色）和系统语言（中文 / 英文）。

术语见 [GLOSSARY.md](GLOSSARY.md)，产品约束见 [PRODUCT.md](PRODUCT.md)，设计决策见 [docs/adr/](docs/adr/)。

## 当前能力

- 内置 new-api 数据来源：PAT（`nap_` 开头）只发 `Authorization: Bearer`；旧版访问令牌另外发 `New-Api-User: <用户 ID>`。
- 金额按站点的显示货币换算（美元、人民币、自定义货币或 token 数）；所有能计入的账户显示货币一致时，页签带最左侧显示「全部账户合计」。只有正常和余额偏低、并且读到了余额的账户计入合计，其余账户注明「n 个账户未计入」；今日花费读不到或单位不同的账户不计入今日合计，合计后注明缺几个，一个都读不到时显示「今日花费无法合计」，不会当作 0。
- 账户状态：正常、余额偏低、部分可用（部分数据读不到，例如余额被站点防护拦截）、需处理（凭据相关的查询全部失败），并给出失败原因和下一步。
- 今日按 **CPA 所在机器的本地时区**从零点算起。
- 账户详情可以选统计区间，显示区间花费、请求数、token 数、平均 RPM/TPM，以及按模型的花费分布、按时间的花费柱状图、调用趋势和按模型用量明细。
- 自定义查询数据来源：粘贴兼容 cc-switch 格式的用量脚本，接入非 new-api 服务或只有 API Key 的情况，账户和内置账户出现在同一排页签里（见下文「自定义查询」）。
- 每个账户可以选「跟随 CPA 代理」（默认，复用 CPA 配置的 `proxy-url`）或「直连」（不使用任何代理），见下文「代理选项」。

账户在页面内的「管理账户」里添加、编辑和删除（见下文），也可以直接调用管理路由。

## 统计区间的口径

- **时区**：所有区间都按 CPA 所在机器的本地时区换算，不按浏览器所在机器。预设区间有今日、昨日、近 7 天（含今天）、近 30 天（含今天）；自定义区间是一对起止日期，含首尾两天，最长 365 天，结束日期不能晚于今天。跨夏令时切换的日子按本地自然日计（那天可能是 23 或 25 小时）。
- **粒度**：区间不超过 2 天按小时分桶，更长按本地自然日分桶。区间包含当前时刻时，分桶截止到现在。
- **数据来源与延迟**：区间花费的主数字来自站点的 `/api/log/self/stat`，是实时的；请求数、token 数、按模型的花费与分布来自站点的数据看板（`/api/data/self`），约有 **5 分钟延迟**，所以区间包含当前时刻时，页面会注明「用量约有 5 分钟延迟」，明细表的合计花费也可能略小于区间花费。
- **平均 RPM / TPM**：区间内的请求数 / token 数除以区间分钟数；区间包含当前时刻时，分钟数只算到现在。实时 RPM / TPM 是站点给出的最近 60 秒数值，不随区间变化。
- **站点未开启数据看板**（`/api/status` 里 `enable_data_export` 为 false）时，站点不记录按模型用量：页面在用量的位置说明原因，请站点管理员在站点设置里开启「数据看板」，开启之后产生的调用才会出现。账户状态不受影响，区间花费照常显示。
- 所选区间的查询失败（例如站点限制了最长可查询的天数，以 `success: false` 拒绝较长的区间）只在详情里显示失败原因和「重试」，**不改变账户状态**；账户状态只由今日的查询决定。

## CPA Core 版本要求

- 最低支持 CPA Core **v8.0.0**。默认的代理选项「跟随 CPA 代理」经宿主回调 `host.http.*` 发出请求，其中用到的 `host.http.operation_open` 和 `host.http.cancel` 从 v8.0.0 起才提供。
- 更早的 Core（例如 v7.3.x）只能在把每个账户的代理选项设为「直连」后使用；「跟随 CPA 代理」的账户在这些版本上查询不到数据。插件不为旧版本提供其他发送方式。
- 本文中在宿主里核对过的行为（例如「CPA 的 request-log 会不会记下凭据」）是在 CPA Core v8.0.78 上核对的，其他 v8 版本没有逐一核对。

## 安装

插件运行在 CPA Core 进程内，先在 CPA 配置里打开插件功能（`plugins.enabled: true`，默认关闭）。

**从商店源安装（推荐）**

1. 添加商店源 `https://raw.githubusercontent.com/qiyi71w/cpa-plugin-newapi-meter/main/registry.json`：在 EasyCLI 的插件商店里添加，或者写进 CPA 配置：

   ```yaml
   plugins:
     enabled: true
     store-sources:
       - "https://raw.githubusercontent.com/qiyi71w/cpa-plugin-newapi-meter/main/registry.json"
   ```

2. 在插件商店里找到 NewAPI Meter 并安装。宿主从本仓库最新的 GitHub Release 下载与 CPA Core 所在平台匹配的 `newapi-meter_<版本>_<goos>_<goarch>.zip`，按 `checksums.txt` 校验后装到 `plugins/<goos>/<goarch>/newapi-meter-v<版本>.<so|dll|dylib>`，并启用插件。安装第三方插件时宿主会弹出来源确认，这是正常的。

**手动放置动态库**

1. 从 [Releases](https://github.com/qiyi71w/cpa-plugin-newapi-meter/releases) 下载与 CPA Core 平台一致的 `newapi-meter_<版本>_<goos>_<goarch>.zip` 和 `checksums.txt`，核对 sha256（例如 `sha256sum -c checksums.txt --ignore-missing`）。
2. zip 根目录只有一个文件 `newapi-meter.so`（Linux）、`newapi-meter.dll`（Windows）或 `newapi-meter.dylib`（macOS）。把它放到 CPA 插件目录下的平台子目录，例如 `plugins/linux/amd64/newapi-meter.so`；也可以改名为带版本号的 `newapi-meter-v<版本>.so`。文件名决定插件 ID，除版本号后缀外不要改动。
3. 在 CPA 配置里启用 `plugins.configs.newapi-meter.enabled: true`（见下文「从源码构建」中的配置示例），然后重启 CPA。

## 升级

- **商店**：在插件商店里安装新版本。新版本写成新的带版本号文件 `newapi-meter-v<新版本>.<扩展名>`，宿主在插件目录里有多个版本时选用版本最高的那个，并清理旧文件。商店提示需要重启时，重启 CPA Core。
- **手动**：Linux 和 macOS 上可以直接替换文件后重启 Core。**Windows 会锁定已加载的 DLL，不能在 Core 运行时覆盖**：要么先停止 Core、替换 DLL、再启动；要么不覆盖旧文件，把新版本放成新的带版本号文件名（例如 `newapi-meter-v1.2.0.dll`），宿主会选用版本最高的文件。在 Windows 上通过商店重装同一个版本也会因为文件被占用而提示需要重启。
- 升级不影响数据目录里的账户和凭据。

## Docker 部署

用 Docker 运行 CPA 时，容器里的默认数据目录（官方镜像以 root 运行，即 `/root/.config/cpa-plugin-newapi-meter`）会随容器重建而丢失。请把数据目录放到挂载的卷上，插件目录也要挂载，否则重建容器后要重新安装插件。挂载的数据目录必须只有所有者能访问（0700，所有者是容器里运行 CPA 的用户），否则插件拒绝使用它（见「数据目录与备份」）；例如在宿主机上先执行 `mkdir -p ./newapi-meter-data && chmod 700 ./newapi-meter-data`：

```yaml
# docker-compose.yml 中 CPA 服务的卷
volumes:
  - ./plugins:/CLIProxyAPI/plugins
  - ./newapi-meter-data:/CLIProxyAPI/newapi-meter-data
```

```yaml
# CPA 配置
plugins:
  enabled: true
  configs:
    newapi-meter:
      enabled: true
      data_dir: /CLIProxyAPI/newapi-meter-data
```

容器内使用 linux 平台的插件，架构与镜像一致（amd64 或 arm64）；从商店安装时宿主会自动选对。

## 平台验证状态

每次推送都在 GitHub Actions 的原生 runner 上构建以下 6 个平台（cgo，不交叉编译），并在各自的 runner 上加载产物，走一遍 register、管理路由调用和 shutdown 的冒烟测试。在真实宿主里的安装与使用验收只在 windows/amd64 上进行。

| 平台 | 验证状态 |
|---|---|
| windows/amd64 | 已通过宿主内验收（EasyCLIProxyAPI 0.3.119，内置 CPA Core v8.1.2） |
| windows/arm64 | 仅经 CI 验证 |
| linux/amd64 | 仅经 CI 验证 |
| linux/arm64 | 仅经 CI 验证 |
| darwin/amd64（Intel） | 仅经 CI 验证 |
| darwin/arm64（Apple Silicon） | 仅经 CI 验证 |

系统要求：Linux 需要 glibc 2.34 及以上（CPA 官方 Docker 镜像满足；不支持 musl，例如 Alpine）；macOS 需要 12 及以上；Windows 版只依赖系统自带的 KERNEL32 和通用 C 运行库（UCRT，Windows 10 起自带）。

## 所需权限

- **PAT**：创建时勾选 `profile:read` 和 `usage:read`。不需要 `wallet:read`。
- **旧版访问令牌**：站点个人设置中的系统访问令牌，必须同时填写账户的数字用户 ID。站点升级到新版鉴权后，旧版访问令牌会在过渡期结束时失效，届时页面会提示改用 PAT。

## 添加账户

1. 打开「NewAPI 余额用量」。还没有账户时，点空状态里的「添加账户」；已有账户时，点账户详情标题行的「管理账户」，再点「添加账户」。
2. 填写表单：
   - **名称**：显示在页签上，用来区分账户。
   - **数据来源**：选「内置 new-api」。非 new-api 服务选「自定义查询」，表单换成脚本和占位符，见下文「自定义查询」。
   - **站点地址**：站点首页的地址，例如 `https://newapi.example.com`。只支持 http 和 https，末尾的斜杠会被去掉。
   - **账户凭据**：PAT（`nap_` 开头）或旧版访问令牌。页面会提示识别出的类型。
   - **用户 ID**：只在填旧版访问令牌时出现，必填，是站点个人设置里显示的数字 ID。
   - **余额阈值**（可选）：余额低于它时，账户显示为余额偏低。单位是这个账户的显示货币，测试连接后会显示出来。
   - **代理**：「跟随 CPA 代理」或「直连」，见下文「代理选项」。
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

## 代理选项

每个账户单独选择访问站点的方式。概况、详情、测试连接、自定义查询和测试脚本的请求都按这个选项发出；测试连接和测试脚本用表单里当前选的选项。

- **跟随 CPA 代理**（默认）：插件把请求交给 CPA，由 CPA 发出（插件宿主的 `host.http.*` 回调），使用 CPA 配置里的 `proxy-url`。CPA 没有配置 `proxy-url` 时，请求和 CPA 自己的其他请求一样：没有代理环境变量就直连；CPA 进程设置了 `HTTP_PROXY` / `HTTPS_PROXY`（以及 `NO_PROXY`）时按它们走。CPA 凭据文件上单独设置的代理不用于插件的请求。
- **直连**：插件用自己的 HTTP 客户端直接访问站点，不使用任何代理：既不用 CPA 的 `proxy-url`，也不读 `HTTP_PROXY` / `HTTPS_PROXY` 等环境变量。适合不需要绕代理的站点。

两种方式的时限、响应上限和失败分类相同：内置来源每个请求 15 秒、响应最大 8 MiB；自定义查询每次 10 秒（含脚本执行）、响应最大 1 MiB。跟随 CPA 代理时，时限由插件计时，到时插件让 CPA 取消还在进行的请求；响应边接收边计数，超过上限就停止接收。

站点是 http（不是 https）地址时，所用代理能看到完整的请求，包括请求头里的凭据。

### 站点重定向到其他地址时

凭据只应发给账户配置的站点。站点返回重定向（3xx）时，两种方式的处理不同：

- **直连**：只跟随仍在同一站点的重定向，即协议、主机和端口都不变，或者同一主机从 http 升级到 https（默认端口）。重定向到其他主机、其他端口或从 https 降到 http 时，插件不跟随，也不向新地址发任何请求，这次查询按收到的重定向响应判为失败：响应是网页时显示为「站点防护拦截」，否则显示为「响应异常」。站点迁到新域名后，请把账户的站点地址（自定义查询的 `baseUrl`）改成新地址。
- **跟随 CPA 代理**：重定向由 CPA 跟随，插件无法阻止。CPA 在跳到其他来源（协议、主机、端口任一不同）时会去掉名称像凭据的请求头：`Authorization`、`Cookie`，以及名称含 `token`、`secret`、`api-key`、`apikey`、`api_key`、`credential` 的头。插件给每个请求设置只含站点来源的 `Referer`（例如 `https://newapi.example.com/`），URL query 里的值不会经 `Referer` 带到新地址；脚本自己设置了 `Referer` 时保留脚本的值。以下内容仍会随重定向发往新地址：
  - 名称不像凭据的请求头，例如内置来源的 `New-Api-User`（用户 ID），以及脚本里自定义名称的头（如 `X-User`）；
  - 307/308 重定向时的请求体。

  自定义查询脚本应把密钥放在 `Authorization` 或名称含 `token`、`api-key` 等的请求头里，或放在 URL query 里。不放心站点会重定向到别处时，可以给账户选「直连」。以上行为在 CPA Core v8.0.78 上核对过。

### CPA 的 request-log 会不会记下凭据

不会。我们在 CPA Core v8.0.78 上开启 `request-log` 和 `logging-to-file` 核对过（另做了一轮再加开 `debug` 的核对）：经「跟随 CPA 代理」发出的内置来源请求（请求头里的 PAT、旧版访问令牌和 `New-Api-User`）和自定义查询请求（分别放在请求头、URL query 和请求体里的值），在 CPA 的日志目录和控制台输出里都找不到。原因是这些请求由插件的管理路由发出，而 CPA 的 request-log 不记录管理路由，也没有记录管理路由里发出的插件请求；页面调用管理路由时提交的凭据（例如测试连接）同样不会被记录。CPA 的访问日志只记一行管理路由的方法、路径和 query（插件的管理路由不在 query 里放凭据）。

这个结论只在 CPA Core v8.0.78 上核对过，其他版本的日志行为可能不同。

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
- 今日花费取第一个提供 `todaySpend` 的项，按这一项的 `unit` 显示，可以和余额的单位不同（例如余额取第一项的美元，今日花费来自人民币套餐）。
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
| 脚本出错（`script_error`） | 语法错误、运行时异常、`request` 不合法（例如不是 http/https 地址），或者 `extractor` 的返回值不符合上面的格式。引擎或脚本自己的报错原样显示；`request` 和返回值哪里不合格由插件判断，页面用当前语言说明 |
| 查询超时（`query_timeout`） | 请求加脚本超过 10 秒（`SCRIPT_TIMEOUT`、`REQUEST_TIMEOUT`），或者这个账户上一次超时的查询还在后台运行（`PREVIOUS_QUERY_RUNNING`，见下文） |
| 脚本报告账户不可用（`reported_invalid`） | `extractor` 返回了 `isValid: false` |
| 响应异常（`bad_response`） | 2xx 响应不是 JSON，或者响应超过 1 MiB |
| 站点无法访问 / 凭据无效 / 站点出错 等 | 请求失败或返回非 2xx，分类与内置来源相同 |

### 限制与风险

- 脚本运行在插件内嵌的纯 Go JavaScript 引擎里，只有 ECMAScript 内置对象：没有 `fetch`、`XMLHttpRequest`、`require`、`import`、`console`、定时器，也不能读写文件。脚本唯一能发出的请求是 `request` 描述的那一个，由插件代发。
- 每次查询（请求加脚本执行）总时限 **10 秒**：到时查询立即返回「查询超时」，正在运行的脚本代码被中断；响应最大 **1 MiB**。
- **脚本运行在 CPA 进程内，内存占用无法限制。** 一个故意或者写错的脚本可以在 10 秒内占用大量内存，影响 CPA 本身。只粘贴你信任的脚本；只有持有 Management Key 的人能保存脚本。
- **引擎无法中断的工作会在超时后继续占用 CPU 和内存，直到它自己结束。** 编译很大的脚本、正则表达式回溯、对大数组排序，以及遮掉、截断脚本返回的超长文本，这些工作中途停不下来：查询照样在 10 秒时返回，但这部分工作留在后台跑完。每个账户同一时间最多留下一个这样的查询：这个账户的新查询（刷新、测试脚本）会先等它结束，10 秒内没等到就返回 `PREVIOUS_QUERY_RUNNING`，不会再启动新的工作；还没保存的账户的测试脚本共用一个名额。
- 请求和内置来源走同一个出站入口，代理选项的效果相同。
- 和账户凭据一样，apiKey、accessToken 明文保存在插件的数据文件里（见「数据目录与备份」）。

## 从源码构建

需要 Go 1.25+、可用的 C 编译器（cgo）和 [bun](https://bun.sh)。

```bash
bun run typecheck                 # 前端类型检查
bun run test:web                  # 前端单元测试（Management Key 的三种存储格式、账户表单校验、统计区间规则）
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

- 账户和凭据保存在数据目录下的 `accounts.json`。默认数据目录是 CPA 运行用户的配置目录下的 `cpa-plugin-newapi-meter`：Windows 为 `%AppData%\cpa-plugin-newapi-meter`；macOS 为 `~/Library/Application Support/cpa-plugin-newapi-meter`；Linux 在设置了 `$XDG_CONFIG_HOME` 时为 `$XDG_CONFIG_HOME/cpa-plugin-newapi-meter`，否则为 `~/.config/cpa-plugin-newapi-meter`。
- 可以用插件配置项 `data_dir`（绝对路径）改到别处；插件会在目录不存在时创建它。请给它一个插件专用的目录。
- Unix 上数据目录必须只有所有者能访问（0700），数据文件为 0600。文件没有额外加密，任何能读这个文件的人都能拿到账户凭据。具体做法：
  - 插件新建的目录为 0700；默认数据目录始终由插件保持为 0700。
  - 已经存在的 `data_dir` 如果给同组或其他用户留了任何权限（例如 0755），插件**不会改动它，而是拒绝使用**：不读取其中的凭据、不查询站点，页面显示目录位置和原因。执行 `chmod 700 <目录>` 后点「重试」即可，不用重启 CPA。
  - 每次读取前，插件会把对其他用户开放的数据文件（例如以 0644 恢复的备份）改为 0600；改不了时（例如文件属于别的用户）同样拒绝使用并说明原因。
  - Windows 上不检查这些权限位，默认目录位于当前用户自己的 `%AppData%` 下。
- 写入使用同目录临时文件加原子替换。如果数据文件无法解析，或者是更新版本的插件写的（`schema_version` 更高），插件**拒绝覆盖**，页面显示文件位置和原因；修复或从备份恢复后刷新即可，不用重启 CPA。
- 没有导入导出。备份就是复制 `accounts.json`；恢复就是把它放回去（权限会在下次读取时收紧为 0600）。用 Docker 运行 CPA 时，把数据目录挂载到卷上，否则重建容器会丢失账户。
- 卸载插件不会删除数据目录。

## 管理路由（开发者说明）

页面的所有数据都来自插件注册的管理路由，挂在 CPA 管理前缀下，需要 Management Key（`Authorization: Bearer <key>` 或 `X-Management-Key: <key>`）。路由按「方法 + 精确路径」匹配，参数用 query 或 JSON body 传。资源路由不鉴权，只返回静态页面。

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

`GET /accounts` 返回 `{"accounts": […], "data_file": "<数据文件路径>"}`：`data_file` 是数据文件 `accounts.json` 的绝对路径（管理视图在列表下显示它，方便备份）。列表中的账户：`id`、`order`、`name`、`source`、`base_url`、`credential_masked`、`credential_kind`（`pat` / `legacy`）、`user_id`、`low_balance_threshold`、`proxy`、`custom_script`、`custom_placeholders`（`baseUrl`、`userId`、`apiKeyMasked`、`accessTokenMasked`）。完整凭据从不出现在任何响应或插件日志里。

概况（`overview`）中每个账户的主要字段：`state`（`ok` / `low` / `partial` / `needs_action`）、`currency`（`code` 为 `USD`/`CNY`/`CUSTOM`/`TOKENS`，自定义查询的其他单位为 `UNIT`，`symbol` 是单位原文）、`balance`、`total_used`、`today_spend`、`rpm`、`tpm`（读不到时为 `null`）、`today_spend_currency`（`today_spend` 的单位，形状同 `currency`；`today_spend` 为 `null` 时也为 `null`；内置账户等于 `currency`，自定义查询账户是提供今日花费的那一项的单位）、`low_balance_threshold`、`balance_failure` / `today_failure`（对应数字读不到的原因）、`primary_failure`（需处理时的主要原因）、`failures`（失败的接口）、`fetched_at`、`last_success_at`（CPA 本次运行中最近一次成功读取）、`cached`。顶层还有 `generated_at`、`today_start`（CPA 本机时区的今日零点）和 `timezone`。

自定义查询账户另有 `custom`：`balance_basis`（`sum` 合计 / `first` 第一项；只有一项时省略）、`total_quota`、`plans`（每项 `plan_name`、`extra`、`remaining`、`used`、`total`、`today_spend`、`unit`、`currency`、`is_valid`、`invalid_message`）、`invalid_message`（脚本报告不可用时）、`script_error`（引擎或脚本给出的报错原文，已遮掉 apiKey 和 accessToken）、`script_reason`（插件自己判断出的脚本问题，代替 `script_error`：`code` 是稳定的原因代码，如 `url_scheme`、`header_name_invalid`、`field_not_number`；`field` 是涉及的字段，如 `result[1].remaining`，可省略）。

测试连接（`accounts/test`）返回：`source`、`ok`（所有计入的查询都成功）、`state`（`ok` / `partial` / `needs_action`，不做余额阈值判断）、`credential_kind`、`site_version`、`currency`、`data_export`、`failure`（最需要处理的失败，成功时为 `null`）、`failures`（失败的接口）。自定义查询账户的测试（页面上的「测试脚本」）另返回 `custom`，字段同概况里的 `custom`，再加上 `balance`、`total_used`、`today_spend` 和 `today_spend_currency`。请求体的校验规则与保存相同，但不检查 `name` 和 `low_balance_threshold`；校验失败返回 400 `validation_failed`，`id` 不存在返回 404 `account_not_found`。

账户详情（`detail`）的参数：`period` 为 `today`（默认）、`yesterday`、`7d`、`30d` 或 `custom`；`custom` 时 `start` 和 `end` 是 CPA 本机时区的日期 `YYYY-MM-DD`（含首尾两天）。不合法时返回 400 `validation_failed`，`field` 为 `period` / `start` / `end`，`reason` 除通用的 `required`、`invalid`、`unsupported` 外还有 `swapped`（结束早于开始）、`too_long`（超过 365 天）、`future`（晚于今天）；自定义查询账户返回 `field: "id"`、`reason: "unsupported"`。今日区间（包括起止都是今天的自定义区间）直接复用概况的今日查询和缓存，不再访问站点；其他区间各自缓存 60 秒，`force=1` 跳过缓存。响应字段：

- `period`：`preset`、`start_date`、`end_date`、`days`、`start` / `end`（RFC 3339，带 CPA 本机时区偏移；`end` 是最后一天之后的零点）、`granularity`（`hour` / `day`）、`includes_now`、`minutes`（平均 RPM/TPM 的分母）；顶层还有 `timezone`。
- `currency`、`data_export`、`spend`（区间花费，来自 `/api/log/self/stat`）及 `spend_failure`。
- `usage`：`spend`、`requests`、`tokens`、`avg_rpm`、`avg_tpm`、`models`（按花费从高到低，每项 `model`、`spend`、`share`、`requests`、`tokens`）、`buckets`（每个小时或每天一项：`start`，以及与 `models` 顺序对应的 `spend`、`requests`、`tokens` 数组）；读不到时为 `null`，原因在 `usage_failure`。
- `usage_disabled`：站点未开启数据看板，此时 `usage` 和 `usage_failure` 都为 `null`。
- `fetched_at`、`cached`。区间查询失败只出现在这些字段里，不影响概况中的账户状态。

失败原因 `code`：`site_unreachable`（网络、DNS、TLS 错误或超时）、`legacy_token_retired`、`pat_scope_denied`、`credential_invalid`、`site_blocked`（站点返回 HTML 而不是 JSON）、`site_error`（5xx）、`bad_response`（其余无法使用的响应，包括 `success: false`），以及自定义查询的 `script_error`、`query_timeout`、`reported_invalid`。`diagnostic` 是可以安全展示的诊断代码，例如 `ACCESS_TOKEN_LEGACY_RETIRED`、`HTTP_401`、`HTML_RESPONSE`、`NETWORK_TIMEOUT`；自定义查询另有 `SCRIPT_SYNTAX`、`SCRIPT_RUNTIME`、`SCRIPT_REQUEST`、`REQUEST_URL`、`REQUEST_SCHEME`、`REQUEST_METHOD`、`REQUEST_HEADER`、`SCRIPT_RESULT`、`SCRIPT_TIMEOUT`、`REQUEST_TIMEOUT`、`PREVIOUS_QUERY_RUNNING`、`IS_VALID_FALSE`、`INVALID_JSON`、`RESPONSE_TOO_LARGE`。

错误响应形如 `{"error": {"code": "...", "field": "...", "reason": "...", "path": "..."}}`：`validation_failed`（带 `field` 和 `reason`）、`invalid_json`、`account_not_found`，以及数据文件相关的 `data_file_corrupt`、`data_file_newer`、`data_file_unavailable`（带 `path`，状态码 503）。`data_file_unavailable` 在目录或文件对其他用户开放时带 `reason`：`dir_not_private`（配置的数据目录给同组或其他用户留了权限，`path` 是该目录）或 `file_not_private`（数据文件无法收紧为 0600，`path` 是该文件）。另有三个与具体账户无关的错误：`invalid_request`（400，宿主转来的管理请求无法解析）、`method_not_allowed`（405，用 GET 以外的方法请求资源路由的页面）、`not_found`（404，没有与「方法 + 路径」匹配的路由）。

查询行为：每个账户每种查询的结果在进程内缓存 60 秒，今日的查询（概况和自定义查询）在 CPA 本机时区跨过零点后不再沿用前一天的结果；同一账户的并发查询合并成一次；单个站点请求 15 秒超时；自定义查询整体（请求加脚本）10 秒超时。

## 构建与发布（维护者）

- CI（`.github/workflows/ci.yml`）在每次推送时：检查前端（类型检查、测试、构建）、Go（`go vet`、`go test -race`）和 `registry.json`；在 6 个原生 runner 上构建并做加载冒烟；再按 Release 的同一套规则打包，作为运行产物 `newapi-meter_0.0.0-ci.<运行编号>` 上传（6 个 zip 加 `checksums.txt`），不发布。
- 推送 `v<semver>` tag 时，同一个工作流以 tag 去掉 `v` 后的版本号构建和打包，最后由发布 job 把这次运行打包好的产物原样发布为 GitHub Release；版本号带预发布后缀（如 `-rc.1`）时标记为预发布，商店不会把它当作最新版本。只有发布 job 拥有 `contents: write` 权限。
- 本地核对：`python3 scripts/check_registry.py` 检查商店源；`gh run download <运行 ID> -n newapi-meter_<版本> -D <目录>` 下载打包产物后，`python3 scripts/package.py verify <版本> <目录>` 检查文件名、zip 内容和 `checksums.txt`。

## 许可证

[MIT](LICENSE)
