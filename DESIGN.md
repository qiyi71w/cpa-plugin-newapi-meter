---
name: NewAPI Meter
description: 嵌在 CPA 宿主里的账户余额、花费与用量仪表
colors:
  bg: "#ffffff"
  surface: "#f6f8f6"
  surface-2: "#eef2ef"
  raised: "#ffffff"
  text: "#0f172a"
  text-2: "#475569"
  text-3: "#5f6e82"
  border: "#e2e8f0"
  border-strong: "#cbd5e1"
  accent: "#222222"
  accent-fg: "#ffffff"
  ok: "#2f8a4b"
  warn: "#c77b30"
  warn-text: "#8b5b21"
  warn-bg: "#fff7e9"
  info: "#3a67c9"
  info-text: "#2f58b0"
  info-bg: "#eff4fd"
  danger: "#b0382a"
  danger-text: "#9f2d20"
  danger-bg: "#fff1ee"
  series-1: "#3b6fd8"
  series-2: "#1a8f7c"
  series-3: "#c27a12"
  series-4: "#c4566b"
  series-5: "#7a5cc9"
  series-6: "#5f8233"
  series-other: "#94a3b8"
typography:
  title:
    fontFamily: "-apple-system, BlinkMacSystemFont, Segoe UI Variable Text, Segoe UI, PingFang SC, Microsoft YaHei UI, Noto Sans SC, Noto Sans CJK SC, sans-serif"
    fontSize: "20px"
    fontWeight: 650
    lineHeight: 1.3
    letterSpacing: "-0.015em"
  headline:
    fontFamily: "-apple-system, BlinkMacSystemFont, Segoe UI Variable Text, Segoe UI, PingFang SC, Microsoft YaHei UI, Noto Sans SC, Noto Sans CJK SC, sans-serif"
    fontSize: "16px"
    fontWeight: 650
    lineHeight: 1.4
  body:
    fontFamily: "-apple-system, BlinkMacSystemFont, Segoe UI Variable Text, Segoe UI, PingFang SC, Microsoft YaHei UI, Noto Sans SC, Noto Sans CJK SC, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.5
    fontFeature: "tnum"
  body-sm:
    fontFamily: "-apple-system, BlinkMacSystemFont, Segoe UI Variable Text, Segoe UI, PingFang SC, Microsoft YaHei UI, Noto Sans SC, Noto Sans CJK SC, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.5
  label:
    fontFamily: "-apple-system, BlinkMacSystemFont, Segoe UI Variable Text, Segoe UI, PingFang SC, Microsoft YaHei UI, Noto Sans SC, Noto Sans CJK SC, sans-serif"
    fontSize: "12px"
    fontWeight: 600
    lineHeight: 1.5
  mono:
    fontFamily: "ui-monospace, Cascadia Mono, SF Mono, Consolas, Liberation Mono, monospace"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.55
rounded:
  gauge: "2px"
  segment: "6px"
  control: "8px"
  tab: "9px"
  region: "12px"
  pill: "999px"
spacing:
  band-gap: "4px"
  band-pad: "5px"
  field-inner: "6px"
  field: "18px"
  panel: "22px"
  section: "26px"
  page-x: "clamp(16px, 2.4vw, 28px)"
  page-top: "20px"
  page-top-cpa: "88px"
components:
  button-primary:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.accent-fg}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.control}"
    padding: "0 12px"
    height: "32px"
  button-secondary:
    backgroundColor: "{colors.raised}"
    textColor: "{colors.text}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.control}"
    padding: "0 12px"
    height: "32px"
  button-secondary-hover:
    backgroundColor: "{colors.surface-2}"
  button-quiet:
    textColor: "{colors.text-2}"
    rounded: "{rounded.control}"
    padding: "0 10px"
    height: "32px"
  button-small:
    backgroundColor: "{colors.raised}"
    textColor: "{colors.text}"
    rounded: "{rounded.control}"
    padding: "0 10px"
    height: "28px"
  button-danger:
    backgroundColor: "{colors.danger-bg}"
    textColor: "{colors.danger-text}"
    rounded: "{rounded.control}"
    padding: "0 12px"
    height: "32px"
  input:
    backgroundColor: "{colors.raised}"
    textColor: "{colors.text}"
    typography: "{typography.mono}"
    rounded: "{rounded.control}"
    padding: "0 10px"
    height: "36px"
  tab-band:
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.region}"
    padding: "5px"
  account-tab:
    textColor: "{colors.text}"
    rounded: "{rounded.tab}"
    padding: "10px 12px 11px"
    width: "172px"
  account-tab-selected:
    backgroundColor: "{colors.raised}"
    textColor: "{colors.text}"
    rounded: "{rounded.tab}"
  period-segment:
    backgroundColor: "{colors.surface-2}"
    textColor: "{colors.text-2}"
    rounded: "{rounded.control}"
    padding: "2px"
  period-segment-active:
    backgroundColor: "{colors.raised}"
    textColor: "{colors.text}"
    rounded: "{rounded.segment}"
    padding: "0 11px"
    height: "28px"
  note:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text}"
    rounded: "{rounded.region}"
    padding: "14px 18px"
  alert-needs-action:
    backgroundColor: "{colors.danger-bg}"
    textColor: "{colors.danger-text}"
    rounded: "{rounded.region}"
    padding: "18px 20px"
  dialog:
    backgroundColor: "{colors.raised}"
    textColor: "{colors.text}"
    rounded: "{rounded.region}"
    padding: "20px 22px"
    width: "440px"
---

# Design System: NewAPI Meter

本文件描述已经构建出来的界面（`web/src/styles.css` 与各组件）。样式表是唯一的取值来源；上面的 frontmatter 记录默认皮肤（EasyCLI 浅色，也是 `:root` 的回退值）和组件尺寸，其余四套皮肤的取值见「Colors」。

## Overview

**Creative North Star: "The Quiet Meter（安静的仪表）"**

NewAPI Meter 不是一个独立网站，而是插在 EasyCLI 插件页或 CPA WebUI 框架里的一块仪表。它借用宿主的底色、字体和明暗，只在一处表达自己：顶部的账户页签带，每个页签底边一条 3px 的余量刻度。用户打开页面几秒钟，扫一眼这一排就知道谁快没钱、今天花了多少，然后再往下看某个账户的明细。

整体是 Operate 模式的密集工具界面：系统字体、表格数字、细线分隔，几乎没有卡片和阴影。颜色几乎都是中性色；只有账户状态（余额偏低、部分可用、需处理）和图表的模型序列有色彩，所以警示色一出现就有意义。界面跟随宿主切换五套皮肤，组件本身只认语义 token，从不写死某个皮肤的颜色。

**Key Characteristics:**
- 宿主的一部分：底色、字体、明暗都跟宿主走，页面没有自己的品牌色。
- 一个标志：页签带和余量刻度；其他地方都克制。
- 状态色只表达状态，数字永远用中性色，除非它本身就是警示（偏低的余额）。
- 细线和色阶分层，阴影只给「被选中」和「浮在上面」的东西。
- 中英文同一套布局，长文本截断或换行，不撑破宽度。

## Colors

调色板是宿主调色板的映射：中性色占绝大部分，三个状态色族各有「图形 / 文字 / 底色」三档，正常状态只有一个绿色点，另有六个模型序列色。

### Primary
- **Ink Accent**（`accent`，配 `accent-fg`）：主按钮的底色（「添加账户」「保存修改」「继续」）、单选框的 accent-color。它就是各皮肤的正文色，所以主按钮读起来是「墨色块」，不是彩色按钮。

### Neutral
- **Page Ground**（`bg`）：页面底色，与宿主内容区一致。
- **Band Surface**（`surface`）：页签带、空状态、说明块、测试结果的底。
- **Recessed Surface**（`surface-2`）：统计区间分段控件的槽、骨架屏、占比条和刻度的空轨道。
- **Raised Paper**（`raised`）：选中的页签、按下的分段、输入框、按钮、对话框、提示浮层，即一切「浮起来」的表面。
- **Ink / Secondary Ink / Tertiary Ink**（`text` / `text-2` / `text-3`）：正文与数字 / 标签、说明、未选中页签的名称 / 坐标轴、分隔点、占位符。
- **Hairline / Strong Hairline**（`border` / `border-strong`）：表格行线、指标带分隔 / 表头底线、输入框与按钮描边、合计格右侧竖线。

### 状态色
- **Healthy Green**（`ok`）：只用于「正常」的 6px 色点和测试成功的勾。正常不需要强调，文字保持中性。
- **Low Amber**（`warn`、`warn-text`、`warn-bg`）：余额偏低。页签的余额数字、刻度填充与阈值竖线、状态文字、指标带里的余额与「低于余额阈值」、统计区间的上限 / 未来日期提示、区间查询失败说明、自定义脚本的风险提示。
- **Partial Blue**（`info`、`info-text`、`info-bg`）：部分可用。空心色点、「无法读取余额」下方的原因、部分可用说明块、测试结果的部分成功；也用于中性的信息提示（自定义区间起止已对调）和文本选区。
- **Needs-Action Red**（`danger`、`danger-text`、`danger-bg`）：需处理。实心红点、「读取失败」、整块的需处理提示、表单字段错误、删除按钮与表单级错误。

### 图表序列
`series-1` 到 `series-6` 依次分给花费最多的六个模型，第七个起合并为「其他」并使用 `series-other`。深色皮肤（EasyCLI 深色、CPA 深色）使用更亮的一组（`#6f9bff`、`#3cc7ae`、`#f0b04a`、`#ef7f94`、`#a58bf0`、`#9cc26a`）；「其他」的中性色按皮肤调整：EasyCLI 浅色 `#94a3b8`、EasyCLI 深色 `#64748b`、CPA 暖灰与纯白 `#a29c95`、CPA 深色 `#79736b`。

### 五套皮肤
`web/src/theme.ts` 在 `<html data-skin>` 上选皮肤：在 CPA WebUI 的同源 iframe 里跟随父页面的 `data-theme`（缺省为暖灰，`white` 为纯白，`dark` 为深色），监听它的变化实时切换，并设置 `data-host="cpa"`；在 EasyCLI WebView 或独立打开时跟随 `prefers-color-scheme`，使用 EasyCLI 的浅色 / 深色调色板。

| token | easycli-light | easycli-dark | cpa-warm | cpa-white | cpa-dark |
|---|---|---|---|---|---|
| `bg` | #ffffff | #0b0d11 | #faf9f5 | #ffffff | #151412 |
| `surface` | #f6f8f6 | #141720 | #f0eee8 | #f5f5f4 | #1d1b18 |
| `surface-2` | #eef2ef | #1c212c | #e9e6df | #ececeb | #262320 |
| `raised` | #ffffff | #1f2531 | #fffdf9 | #ffffff | #2a2723 |
| `text` | #0f172a | #f8fafc | #2d2a26 | #2d2a26 | #f6f4f1 |
| `text-2` | #475569 | #b0bdcf | #625c55 | #625c55 | #c9c3bb |
| `text-3` | #5f6e82 | #93a2b8 | #6f6961 | #6f6961 | #a39c93 |
| `border` | #e2e8f0 | #222836 | #e3e1db | #e5e5e5 | #3a3530 |
| `border-strong` | #cbd5e1 | #313a4b | #d2cec6 | #d4d4d4 | #4a453f |
| `accent` / `accent-fg` | #222222 / #ffffff | #f5f5f5 / #0b0d11 | #2d2a26 / #faf9f5 | #2d2a26 / #ffffff | #f6f4f1 / #151412 |
| `ok` | #2f8a4b | #34d399 | #10a674 | #10a674 | #22c58b |
| `warn` / `warn-text` | #c77b30 / #8b5b21 | #e08a1e / #fcd34d | #d39d0c / #7f5d06 | #d39d0c / #7f5d06 | #f2c94c / #ffd862 |
| `info` / `info-text` | #3a67c9 / #2f58b0 | #7fa2ff / #a9c0ff | #3d6bc4 / #335ca8 | #3d6bc4 / #335ca8 | #86a6f5 / #a9bff7 |
| `danger` / `danger-text` | #b0382a / #9f2d20 | #d0604e / #fca5a5 | #c65746 / #9e3b2b | #c65746 / #9e3b2b | #d0624f / #eb9a89 |

深色皮肤和 CPA 皮肤的 `*-bg` 是半透明的状态色（如 CPA 暖灰的 `warn-bg` 为 `rgba(224, 170, 20, 0.14)`），叠在当前底色上，所以同一个状态在每套皮肤里都带着各自宿主的色温。纯白与暖灰只差底色、表面与边线，文字色和状态色相同。

### Named Rules
**The Status-Only Color Rule.** 色彩只表达账户状态和模型序列。金额、请求数、token 数用正文色；只有余额偏低时，余额数字和刻度才换成警示色。

**The Three-Step Status Rule.** 每个状态色族分三档：图形用 `warn` / `info` / `danger`（色点、刻度、描边），文字用 `*-text`，大面积只用 `*-bg`。文字从不直接用图形档。

**The Skin-Agnostic Rule.** 组件只写 `var(--nm-*)`。新增颜色先在五套皮肤里都定义语义 token，再在组件里使用。

## Typography

**Display Font:** 无；标题用正文字体加粗。
**Body Font:** 系统无衬线字体栈（`-apple-system`、Segoe UI Variable Text、Segoe UI、PingFang SC、Microsoft YaHei UI、Noto Sans SC 等）
**Label/Mono Font:** 系统等宽字体栈（ui-monospace、Cascadia Mono、SF Mono、Consolas）

**Character:** 与宿主相同的系统字体，不加载任何字体文件。整页开启表格数字（`tabular-nums`），金额与计数上下对齐、刷新时不跳动。

### Hierarchy
- **Title**（650，20px，1.3，-0.015em）：详情标题（账户名）、管理视图标题、页签上的余额数字。
- **Headline**（650，16px）：空状态标题、需处理提示标题、对话框标题；指标带的数值用 600、16px。
- **Body**（400，14px，1.5）：正文、名称输入框。
- **Body-sm**（13px）：按钮、表格、图例、页签名称、元信息行、说明块、表单标签与选项名。
- **Label**（12px）：指标标签、状态文字、页签副行（今日花费）、表头、字段说明、区间说明；坐标轴 11px。
- **Mono**（13px，说明里 12px）：地址、凭据、用户 ID 等输入框，脚本编辑区，诊断代码，站点主机名，数据文件路径。

说明和错误文字限制在 68ch，诊断元信息 76ch。

### Named Rules
**The Weight-Not-Size Rule.** 层级主要靠 600 / 650 的字重和文字色深浅区分，字号只有 12、13、14、16、20px 五级（坐标轴 11px），不加新字号。

**The Mono-Means-Machine Rule.** 等宽字体只给机器读的东西：地址、凭据、脚本、诊断代码、路径。金额和名称永远用正文字体。

## Layout

页面是单列，宽度随宿主内容区变化，验收宽度为 760 / 920 / 1080px（760 和 1080 是宿主侧栏决定的上下限）。`.nm` 是一个 `inline-size` 容器（容器名 `nm`），响应式都用容器查询，不看视口：

- 页面内边距：上 20px、两侧 `clamp(16px, 2.4vw, 28px)`、下 40px；在 CPA WebUI 里顶部留 88px，避开宿主浮在框架右上角的工具栏。
- 页签带：合计格（不收缩，最宽 220px）加可横向滚动的页签条。每个页签 `flex: 1 0 172px`、最宽 260px，放不下时横向滚动，不换行。
- 详情区距页签带 22px。标题行左侧（名称、状态胶囊、元信息）与右侧按钮（刷新、管理账户）可以换行。
- 指标带：默认 4 列；内置来源 7 格时排成 4 + 3 两行，容器宽度 ≥ 960px 时一行 7 格；容器 ≤ 600px 时 2 列。
- 两张图表在容器 ≥ 840px 时并排（列间距 32px），更窄时上下排；图表高 216px。
- 用量分区之间 26px；表单字段间 18px，字段内部 6px，操作行前 22px；表单最宽 600px，空状态与 key 输入框最宽 560px。

### Named Rules
**The No-Sideways-Page Rule.** 页面本身永远没有横向溢出。允许横向滚动的只有页签条和按模型用量表的外框；长名称、长主机名一律省略号或在字内换行。

## Elevation & Depth

以色阶分层为主：`bg` → `surface` → `surface-2` 的凹槽，`raised` 作为浮起的纸面，区块之间用 1px 细线。阴影只有两种，都带偏移和柔和模糊。

### Shadow Vocabulary
- **Lift**（`--nm-shadow`，每套皮肤一份，例如 EasyCLI 浅色 `0 1px 2px rgba(24,32,42,0.06), 0 4px 12px -4px rgba(24,32,42,0.1)`）：选中的页签、按下的统计区间分段、页签条的滚动箭头、图表提示浮层（另加 1px 描边环）。
- **Dialog**（`0 2px 6px rgba(0,0,0,0.12), 0 18px 40px -12px rgba(0,0,0,0.35)`）：只给删除确认对话框，背景压暗为 `rgba(12,12,14,0.42)`。

### Named Rules
**The Selected-Floats Rule.** 静止的表面一律平铺。阴影只表示「当前选中」或「浮在内容上面」；说明块、表格、指标带、表单选项都不加阴影。

## Shapes

- 控件 8px（按钮、输入框、日期框、分段槽、表单选项、风险提示、提示浮层）；区域 12px（页签带、说明块、需处理提示、测试结果、空状态、对话框）。
- 页签 9px，嵌在 5px 内边距的页签带里，与带的 12px 外角同心；分段按钮 6px，嵌在 2px 的槽里。
- 胶囊 999px 只给小东西：状态胶囊、「合计」/「第一项」标签、滚动箭头、套餐的无效标记。
- 刻度和占比条 2px 圆角；色点 6px 圆；图例色块 8px、2px 圆角；柱子顶端最多 1px 圆角。
- 图标为自绘的 24×24 线性 SVG，描边 1.75。

## Components

### Buttons
- **Shape:** 8px 圆角，高 32px（表格内小按钮 28px），水平内边距 12px，13px / 600。
- **Primary:** `accent` 底、`accent-fg` 字（添加账户、保存修改、继续）。每个视图最多一个；需处理提示里，凭据类失败时「替换凭据」是主按钮，其他失败时「重试」是主按钮。
- **Secondary:** `raised` 底、`border-strong` 描边、正文色（刷新、管理账户、测试连接、取消、重试）。
- **Small / Quiet / Quiet-danger:** 列表行内的「编辑」是 28px 高的小号次级按钮，「删除」是透明底、`danger-text` 字的小号安静按钮；「返回仪表盘」是透明底、次级文字色的安静按钮。
- **Danger:** `danger-bg` 底、半透明 `danger` 描边、`danger-text` 字，只用于对话框里的「删除账户」。
- **Hover / Focus / Busy:** 悬停换到 `surface-2`（主按钮混入 14% 底色），180ms；焦点环为 2px、55% 正文色、偏移 2px；进行中的按钮禁用、图标旋转并设 `aria-busy`。

### Inputs / Fields
- **Style:** `raised` 底、`border-strong` 1px 描边、8px 圆角、高 36px、等宽 13px；名称用正文字体 14px；脚本编辑区至少 220px 高、可纵向拉伸、不自动换行。
- **Focus:** 全局焦点环（偏移 1px）；悬停时描边变为 `text-3`。
- **Hints / Errors:** 说明在字段下方（12px，次级色）；错误紧跟字段（13px / 600，`danger-text`，带图标），描边变 `danger`；提交出错时焦点移到第一个错误字段。凭据字段只显示打码占位，留空表示保留。
- **Choices:** 数据来源和代理选项是整块可点的选项卡片：选中时描边加深、底色换成 `surface`，焦点环画在整块上；按 `minmax(220px, 1fr)` 自动换行。

### Account Tab Band（标志组件）
- **结构:** 合计格（可选）加页签条。每个页签依次是：名称（省略号）与状态（色点加文字）、余额（20px / 650）、副行（今日花费，多套餐时前面带「合计」或「第一项」标签）、底边刻度。
- **余量刻度:** 3px 高的轨道，阈值竖线固定在 1/4 处，填满表示余额达到阈值的 4 倍，填充宽度有 700ms 过渡。五种形态：正常为次级文字色填充；余额偏低时填充、竖线和余额数字都换成警示色；没有设阈值时画成 1px 虚线、不填充；部分可用时是 2px 蓝色点线，余额位置写「无法读取余额」和原因；需处理时整条轨道为淡红色，余额位置写「读取失败」，副行写上次成功时间。
- **选中:** `raised` 底加 Lift 阴影，名称换成正文色；键盘 ←/→（循环）、Home、End 切换，并把选中页签滚入可见区域。
- **合计格:** 只在所有能计入合计的账户显示货币一致时出现，不可选中；右侧一条 `border-strong` 竖线把它和页签条分开；显示余额合计和今日合计，有不计入的账户时再加一行说明。
- **溢出:** 页签条隐藏滚动条；能继续滚动的一侧显示 64px 渐隐（从 `surface` 到透明）和 28px 圆形箭头按钮（点击滚动 70% 宽度，不进入 Tab 顺序）。

### Detail（内置来源）
- **标题行:** 账户名（20px）、非正常状态时的状态胶囊、元信息行（站点主机名、凭据类型、站点版本、上次更新，中间用 `text-3` 的「·」分隔），右侧「刷新」「管理账户」。
- **统计区间:** 分段控件（今日、昨日、近 7 天、近 30 天、自定义），按下的分段浮起；选自定义后出现两个日期框，下方一行提示（对调用 `info-text`，超出上限或晚于今天用 `warn-text` 加粗）；右侧说明区间和粒度（按小时 / 按天），区间包含当前时刻时注明约 5 分钟延迟。切换区间时旧数据保留并降到 55% 不透明度，直到新数据到达。
- **指标带:** 上下两条细线之间的网格，格子之间 1px 竖线，没有卡片。依次为区间花费、请求数、token 数、平均 RPM/TPM，再接余额（附阈值）、累计已用、实时 RPM/TPM（最近 60 秒）；一行排开时，余额与前四格之间的竖线加深。
- **按模型的花费分布:** 8px 高的 100% 占比条（段间 2px），下面是图例（色块、模型名、占比、金额）。
- **图表:** 自绘 SVG。左侧是按时间的堆叠柱（柱宽不超过 28px），右侧是调用趋势折线（2px），右上角的小分段控件在请求数和 token 数之间切换。网格线是 `border` 色的 2/4 虚线；悬停时整列加 6% 正文色的底，提示浮层始终留在图表框内。
- **按模型用量明细:** 数字右对齐的细线表格（花费、带 48px 小进度条的占比、请求数、token 数），表尾合计行加粗；外框可横向滚动。区间内没有用量、未开启数据看板、区间查询失败时，用说明块代替图表（查询失败用 `warn-bg`）。
- **部分可用 / 需处理:** 部分可用时，能读到的部分照常显示，读不到的格子写原因，下方 `info-bg` 说明块列出读不到的部分和诊断代码；需处理时显示整块 `danger-bg` 提示：错误标题、下一步、上次成功时间、诊断代码，以及「替换凭据」「重试」。

### Custom Query Detail
与内置详情共用标题行和指标带：余额（多套餐时带「合计」或「第一项」标签，附阈值）、累计已用、总额度、今日花费；下面是套餐表（名称与附加说明左对齐，数字右对齐，无效套餐置灰并加红色小标记），最后一行小字说明这类账户没有统计区间和图表。USD、CNY 以外的单位跟在数字后面，小一号、次级色。

### Manage Accounts View
- 页内独立视图（不用模态框）。左上角是安静按钮「返回仪表盘」；进入视图时标题获得焦点，读屏从新视图开始。
- **列表:** 细线表格：名称（加粗，省略号）、数据来源、站点主机名（等宽，省略号）、状态，行尾是编辑 / 删除安静按钮；表下注明数据文件位置并提示备份方式。
- **表单:** 字段依次为名称、数据来源，内置来源接站点地址、账户凭据、用户 ID（只在旧版访问令牌时出现），自定义查询接脚本编辑区、风险提示、占位符取值，然后是余额阈值和代理选项；操作行为主按钮、测试连接 / 测试脚本、取消。
- **测试结果:** 操作行下方的区域块：成功为 `surface` 底加绿色勾，部分成功为 `info-bg`，失败为 `danger-bg`；用定义列表列出凭据类型、站点版本、显示货币，失败时给出标题、下一步和诊断代码；脚本报错原文放在引用框里（等宽，最多 180px 高）。
- **风险提示:** `warn-bg` 底的提示块，加粗的第一句说明脚本运行在 CPA 进程内、内存占用无法限制。
- **删除确认:** 整个界面唯一的对话框，宽 440px，标题写出账户名，默认焦点在「取消」，确认按钮用 Danger 样式。

### Whole-Page States
空状态和缺少 Management Key 时的输入框都是 `surface` 底的 12px 区块（最宽 560px）：一个标题、一段说明、一个主按钮或输入行。首次加载显示脉动的骨架页签和一行状态文字；数据文件无法读取等整页问题用 `warn-bg` 横幅。

### Motion
- 唯一的入场动效：切换账户时详情区 260ms 淡入上移（从 0.35 不透明度、下移 6px 开始），缓动 `cubic-bezier(0.23, 1, 0.32, 1)`。
- 状态反馈：悬停、选中、分段切换 180ms；刻度填充宽度 700ms；刷新图标 900ms 旋转；骨架 1.4s 脉动；页签条平滑滚动。
- 系统开启「减少动态效果」（`prefers-reduced-motion: reduce`）时，关闭入场动效、旋转、脉动和所有过渡，页签条直接跳转。

### i18n
- 中文和英文两套文案表，按 `navigator.language` / `navigator.languages` 选择（`zh` 开头为中文，`en` 开头为英文），判断不出时用中文。英文表与中文表的键集合由类型检查保证一致。
- 数字、金额、日期、相对时间都用 `Intl` 按界面语言格式化（中文用「万」，英文用 K / M）。
- 布局按两种语言的实际长度设计：页签名、图例名、模型名用省略号；说明、错误、提示换行并限制宽度；按钮文字不换行。

## Do's and Don'ts

### Do:
- **Do** 只用 `--nm-*` 语义 token；新颜色在五套皮肤里同时定义。
- **Do** 让余额偏低同时体现在三处：余额数字、刻度填充、阈值竖线（`warn` / `warn-text`）。
- **Do** 部分可用用空心色点和蓝色点线刻度，需处理用实心红点和淡红轨道，正常只有一个 6px 绿点。
- **Do** 用细线和 `surface` / `surface-2` / `raised` 的色阶分层；阴影只给选中和浮层。
- **Do** 在 760 和 1080 宽度、中英文下用真实文案检查：页面不横向溢出，只有页签条和用量表外框可以横向滚动。
- **Do** 把来自站点或脚本的文本作为文本节点渲染；凭据只显示打码值。
- **Do** 在 CPA WebUI 里保留 88px 顶部留白，给宿主的浮动工具栏让位。

### Don't:
- **Don't** 给状态和图表序列以外的元素上色；警示色出现得越少越有用。
- **Don't** 把指标做成一排卡片，也不要给说明块、表格、指标带加阴影。
- **Don't** 再加第二个入场动效；260ms 的账户切换是唯一的。
- **Don't** 用模态框承载添加或编辑账户；模态只留给删除确认。
- **Don't** 加载任何外部字体、图标、图片或脚本，也不要引入图表库；图表是自绘 SVG。
- **Don't** 用超过 1px 的 `border-left` 色条标记状态块，也不要用 emoji 或 Unicode 字符代替图标。
- **Don't** 在示例、截图说明或文案中使用真实站点地址或凭据；用 `example.com` 之类的占位。
