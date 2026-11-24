# 构建与发布

[返回 README](../README.md)

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

把构建产物放进 CPA 的方法见[手动放置动态库](installation.md#手动放置动态库)。

## CI 与发布

- CI（`.github/workflows/ci.yml`）在每次推送时：检查前端（类型检查、测试、构建）、Go（`go vet`、`go test -race`）和 `registry.json`；在 6 个原生 runner 上构建并做加载冒烟；再按 Release 的同一套规则打包，作为运行产物 `newapi-meter_0.0.0-ci.<运行编号>` 上传（6 个 zip 加 `checksums.txt`），不发布。
- 推送 `v<semver>` tag 时，同一个工作流以 tag 去掉 `v` 后的版本号构建和打包，最后由发布 job 把这次运行打包好的产物原样发布为 GitHub Release；版本号带预发布后缀（如 `-rc.1`）时标记为预发布，商店不会把它当作最新版本。只有发布 job 拥有 `contents: write` 权限。
- 本地核对：`python3 scripts/check_registry.py` 检查商店源；`gh run download <运行 ID> -n newapi-meter_<版本> -D <目录>` 下载打包产物后，`python3 scripts/package.py verify <版本> <目录>` 检查文件名、zip 内容和 `checksums.txt`。
