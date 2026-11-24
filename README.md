<div align="center">

# NewAPI Meter

**在 CPA 里查看多个 new-api 系站点账户的余额、今日花费和按模型用量**

[![Release](https://img.shields.io/github/v/release/qiyi71w/cpa-plugin-newapi-meter)](https://github.com/qiyi71w/cpa-plugin-newapi-meter/releases) [![Downloads](https://img.shields.io/github/downloads/qiyi71w/cpa-plugin-newapi-meter/total)](https://github.com/qiyi71w/cpa-plugin-newapi-meter/releases) [![CPA Core](https://img.shields.io/badge/CPA%20Core-%E2%89%A5%20v8.0.0-blue)](docs/installation.md#cpa-core-版本要求) [![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)

[功能](#功能) · [快速开始](#快速开始) · [自定义查询](#自定义查询) · [文档](#文档) · [从源码构建](#从源码构建)

</div>

NewAPI Meter 是 [CPA（CLIProxyAPI）](https://github.com/router-for-me/CLIProxyAPI) 的原生插件。装好之后，CPA WebUI 或 EasyCLIProxyAPI 的插件菜单里会多出「NewAPI 余额用量」。你配置的账户排成一排页签，每个账户显示状态、余额、今日花费，以及余额离你设定的阈值还有多远。不用再挨个登录站点后台。

插件读的是**账户级**数据（余额、累计已用、今日花费、实时 RPM/TPM），口径与站点后台一致。按 API Key 查额度请用 balance-query 一类插件。

## 功能

- **多账户总览**：账户状态分为正常、余额偏低、部分可用、需处理，读取失败时给出原因和下一步。所有账户显示货币一致时，页签带最左侧显示「全部账户合计」。
- **按站点货币显示**：金额按站点设定换算成美元、人民币、自定义货币或 token 数。
- **统计区间**：今日、昨日、近 7 天、近 30 天，或最长 365 天的自定义区间。详情页给出区间花费、请求数、token 数、平均 RPM/TPM，以及按模型的花费分布、花费柱状图、调用趋势和用量明细。
- **自定义查询**：粘贴兼容 cc-switch 格式的用量脚本，接入非 new-api 服务，或者只有 API Key 的账户。
- **按账户选代理**：跟随 CPA 配置的 `proxy-url`，或者直连。
- **凭据单独存放**：账户凭据只保存在插件自己的数据文件里，不写进 CPA 配置；页面和管理接口只返回打码值。
- **按需查询**：只在打开页面或点「刷新」时访问站点，不在后台定时拉取。
- **跟随宿主外观**：主题跟随 CPA WebUI（暖灰 / 纯白 / 深色）或系统浅色 / 深色，界面语言按系统语言选择中文或英文。

## 运行要求

| 项目 | 要求 |
|---|---|
| CPA Core | v8.0.0 及以上 |
| 平台 | Windows、Linux、macOS 的 amd64 与 arm64 |
| Linux | glibc 2.34 及以上（CPA 官方 Docker 镜像满足；不支持 musl，例如 Alpine） |
| macOS | 12 及以上 |
| Windows | 只依赖系统自带的 KERNEL32 和 UCRT（Windows 10 起自带） |
| 站点凭据 | PAT（`nap_` 开头，勾选 `profile:read` 和 `usage:read`），或旧版访问令牌加数字用户 ID |

> [!IMPORTANT]
> 默认的代理选项「跟随 CPA 代理」依赖 CPA Core v8.0.0 起提供的宿主回调。更早的 Core（例如 v7.3.x）只能在每个账户都选「直连」时使用。

6 个平台每次推送都在 CI 的原生 runner 上构建并做加载冒烟测试，宿主内验收目前只在 windows/amd64 上做过，详见[平台验证状态](docs/installation.md#平台验证状态)。

## 快速开始

1. 在 CPA 配置里打开插件功能（默认关闭），并添加本仓库的商店源。也可以在 EasyCLI 的插件商店里直接添加这个地址。

   ```yaml
   plugins:
     enabled: true
     store-sources:
       - "https://raw.githubusercontent.com/qiyi71w/cpa-plugin-newapi-meter/main/registry.json"
   ```

2. 在插件商店里找到 **NewAPI Meter** 并安装。宿主会从最新的 GitHub Release 下载对应平台的包，按 `checksums.txt` 校验后启用。安装第三方插件时宿主会弹出来源确认，这是正常的。

3. 打开「NewAPI 余额用量」，点「添加账户」，填写名称、站点地址（例如 `https://newapi.example.com`）和账户凭据，先点「测试连接」确认能读到数据，再保存。

手动放置动态库、升级（含 Windows 的 DLL 锁定问题）和 Docker 部署见[安装、升级与部署](docs/installation.md)。

> [!TIP]
> 用 Docker 运行 CPA 时，把插件的数据目录挂到卷上，并设为 0700。否则重建容器会丢失账户，权限过宽时插件也会拒绝使用该目录。配置示例见 [Docker 部署](docs/installation.md#docker-部署)。

## 账户状态

| 状态 | 含义 |
|---|---|
| 正常 | 所有查询都成功，余额没有低于阈值（未设阈值时不做这项判断） |
| 余额偏低 | 余额低于你给这个账户设的余额阈值 |
| 部分可用 | 部分数据读不到，例如余额接口被站点防护拦截而花费正常；不参与余额阈值判断和余额合计 |
| 需处理 | 需要凭据的查询全部失败，例如凭据失效、PAT 权限不足或站点无法访问；详情里给出主要原因，并可以直接替换凭据 |

「今日」按 CPA 所在机器的本地时区从零点算起。统计区间的分桶粒度、数据看板约 5 分钟的延迟等口径，见[统计区间的口径](docs/usage.md#统计区间的口径)。

## 自定义查询

非 new-api 服务用「自定义查询」数据来源接入。脚本格式兼容 [cc-switch](https://github.com/farion1231/cc-switch) 的用量脚本，它的「通用」和「New API」模板可以原样使用：

```js
({
  request: {
    url: "{{baseUrl}}/user/balance",
    headers: { "Authorization": "Bearer {{apiKey}}" }
  },
  extractor: function (response) {
    return { remaining: response.balance, unit: "USD" };
  }
})
```

自定义查询账户和内置账户在同一排页签里，同样参与余额阈值判断和合计。返回字段、占位符、多套餐规则和失败分类见[自定义查询](docs/custom-query.md)。

> [!WARNING]
> 脚本在 CPA 进程内运行，内存占用无法限制。只粘贴你信任的脚本。只有持有 Management Key 的人能保存脚本。

## 文档

| 文档 | 内容 |
|---|---|
| [安装、升级与部署](docs/installation.md) | CPA Core 版本要求、商店与手动安装、升级、Docker、平台验证状态 |
| [使用指南](docs/usage.md) | 凭据权限、添加账户、测试连接的失败说明、金额合计、统计区间口径、数据目录与备份 |
| [代理、重定向与日志](docs/network.md) | 两种代理选项、站点重定向时凭据的去向、CPA request-log 是否记录凭据 |
| [自定义查询](docs/custom-query.md) | 脚本格式、占位符、测试脚本、限制与风险 |
| [管理路由](docs/management-api.md) | 插件注册的 HTTP 路由、请求与响应字段、错误代码 |
| [构建与发布](docs/development.md) | 本地构建与测试、CI 和 Release 流程 |

术语以 [GLOSSARY.md](GLOSSARY.md) 为准，产品约束见 [PRODUCT.md](PRODUCT.md)，设计决策见 [docs/adr/](docs/adr/)。

## 从源码构建

需要 Go 1.25+、可用的 C 编译器（cgo）和 [bun](https://bun.sh)。

```bash
bun install
bun run typecheck && bun run test:web   # 前端类型检查与单元测试
go test ./...                           # Go 测试，使用合成的假站点，不访问网络
bun run build                           # 先构建页面，再构建插件
```

产物在 `dist/<goos>/<goarch>/newapi-meter.{so,dll,dylib}`。交叉编译、版本号和 CI 发布流程见[构建与发布](docs/development.md)。
