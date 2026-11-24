# 安装、升级与部署

[返回 README](../README.md)

## CPA Core 版本要求

- 最低支持 CPA Core **v8.0.0**。默认的代理选项「跟随 CPA 代理」经宿主回调 `host.http.*` 发出请求，其中用到的 `host.http.operation_open` 和 `host.http.cancel` 从 v8.0.0 起才提供。
- 更早的 Core（例如 v7.3.x）只能在把每个账户的代理选项设为「直连」后使用；「跟随 CPA 代理」的账户在这些版本上查询不到数据。插件不为旧版本提供其他发送方式。
- 文档中在宿主里核对过的行为（例如[CPA 的 request-log 会不会记下凭据](network.md#cpa-的-request-log-会不会记下凭据)）是在 CPA Core v8.0.78 上核对的，其他 v8 版本没有逐一核对。

## 从商店源安装

插件运行在 CPA Core 进程内，先在 CPA 配置里打开插件功能（`plugins.enabled: true`，默认关闭）。

1. 添加商店源 `https://raw.githubusercontent.com/qiyi71w/cpa-plugin-newapi-meter/main/registry.json`：在 EasyCLI 的插件商店里添加，或者写进 CPA 配置：

   ```yaml
   plugins:
     enabled: true
     store-sources:
       - "https://raw.githubusercontent.com/qiyi71w/cpa-plugin-newapi-meter/main/registry.json"
   ```

2. 在插件商店里找到 NewAPI Meter 并安装。宿主从本仓库最新的 GitHub Release 下载与 CPA Core 所在平台匹配的 `newapi-meter_<版本>_<goos>_<goarch>.zip`，按 `checksums.txt` 校验后装到 `plugins/<goos>/<goarch>/newapi-meter-v<版本>.<so|dll|dylib>`，并启用插件。安装第三方插件时宿主会弹出来源确认，这是正常的。

## 手动放置动态库

1. 从 [Releases](https://github.com/qiyi71w/cpa-plugin-newapi-meter/releases) 下载与 CPA Core 平台一致的 `newapi-meter_<版本>_<goos>_<goarch>.zip` 和 `checksums.txt`，核对 sha256（例如 `sha256sum -c checksums.txt --ignore-missing`）。
2. zip 根目录只有一个文件 `newapi-meter.so`（Linux）、`newapi-meter.dll`（Windows）或 `newapi-meter.dylib`（macOS）。把它放到 CPA 插件目录下的平台子目录，例如 `plugins/linux/amd64/newapi-meter.so`；也可以改名为带版本号的 `newapi-meter-v<版本>.so`。文件名决定插件 ID，除版本号后缀外不要改动。
3. 在 CPA 配置中启用插件，然后重启 CPA：

   ```yaml
   plugins:
     enabled: true
     configs:
       newapi-meter:
         enabled: true
         # data_dir: /absolute/path/to/data   # 可选，见「数据目录与备份」
   ```

`data_dir` 的说明见[数据目录与备份](usage.md#数据目录与备份)。

## 升级

- **商店**：在插件商店里安装新版本。新版本写成新的带版本号文件 `newapi-meter-v<新版本>.<扩展名>`，宿主在插件目录里有多个版本时选用版本最高的那个，并清理旧文件。商店提示需要重启时，重启 CPA Core。
- **手动**：Linux 和 macOS 上可以直接替换文件后重启 Core。
- 升级不影响数据目录里的账户和凭据。

> [!WARNING]
> Windows 会锁定已加载的 DLL，不能在 Core 运行时覆盖。要么先停止 Core、替换 DLL、再启动；要么不覆盖旧文件，把新版本放成新的带版本号文件名（例如 `newapi-meter-v1.2.0.dll`），宿主会选用版本最高的文件。在 Windows 上通过商店重装同一个版本也会因为文件被占用而提示需要重启。

## Docker 部署

用 Docker 运行 CPA 时，容器里的默认数据目录（官方镜像以 root 运行，即 `/root/.config/cpa-plugin-newapi-meter`）会随容器重建而丢失。请把数据目录放到挂载的卷上，插件目录也要挂载，否则重建容器后要重新安装插件。

挂载的数据目录必须只有所有者能访问（0700，所有者是容器里运行 CPA 的用户），否则插件拒绝使用它（见[数据目录与备份](usage.md#数据目录与备份)）。例如在宿主机上先执行 `mkdir -p ./newapi-meter-data && chmod 700 ./newapi-meter-data`：

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
