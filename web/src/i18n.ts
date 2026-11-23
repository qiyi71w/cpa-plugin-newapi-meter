// Every UI string lives here in Chinese and English. Add new strings to `zh`
// first; the `en` table is typed against it, so a missing translation fails
// the typecheck. Placeholders are written {name} and filled by t().

const zh = {
  "page.title": "余额用量 · NewAPI Meter",

  "state.ok": "正常",
  "state.low": "余额偏低",
  "state.lowShort": "余额偏低",
  "state.partial": "部分可用",
  "state.needs_action": "需处理",

  "band.label": "账户",
  "band.scrollLeft": "向左滚动账户",
  "band.scrollRight": "向右滚动账户",
  "band.today": "今日 {amount}",
  "band.readFailed": "读取失败",
  "band.lastSuccess": "上次成功 {when}",
  "band.neverSucceeded": "尚未成功读取",
  "band.balanceUnavailable": "无法读取余额",
  "band.unsupported": "不支持的数据来源",
  "gauge.ratio": "余额是余额阈值 {threshold} 的 {ratio} 倍；刻度线为阈值，满格为 4 倍",
  "gauge.noThreshold": "未设余额阈值",
  "gauge.unknown": "部分可用的账户不做余额阈值判断",

  "total.label": "全部账户合计",
  "total.today": "今日 {amount}",
  "total.excludedOne": "{n} 个账户未计入",
  "total.excludedMany": "{n} 个账户未计入",
  "total.excludedHint": "部分可用和需处理的账户不计入合计",

  "credential.pat": "PAT",
  "credential.legacy": "旧版访问令牌",
  "detail.siteVersion": "站点 {version}",
  "detail.updated": "上次更新 {when}",
  "detail.lastAttempt": "上次尝试 {when}",
  "detail.refresh": "刷新",
  "detail.refreshing": "正在刷新",
  "detail.refreshHint": "重新查询全部账户，不使用缓存",

  "metric.balance": "余额",
  "metric.threshold": "余额阈值 {amount}",
  "metric.belowThreshold": "低于余额阈值 {amount}",
  "metric.noThreshold": "未设余额阈值",
  "metric.thresholdSkipped": "部分可用，不做阈值判断",
  "metric.totalUsed": "累计已用",
  "metric.todaySpend": "今日花费",
  "metric.realtime": "实时 RPM / TPM",
  "metric.realtimeSub": "最近 60 秒",
  "metric.unavailable": "无法读取",

  "partial.title": "部分数据没有读到",
  "partial.item": "{part}：{reason}",
  "part./api/status": "站点设置（金额换算）",
  "part./api/user/self": "余额与累计已用",
  "part./api/log/self/stat": "今日花费与实时 RPM/TPM",
  "part./api/data/self": "按模型用量",

  "failure.site_unreachable": "站点无法访问",
  "failure.legacy_token_retired": "旧版访问令牌已失效",
  "failure.pat_scope_denied": "PAT 权限不足",
  "failure.credential_invalid": "凭据无效",
  "failure.site_blocked": "站点防护拦截",
  "failure.site_error": "站点出错",
  "failure.bad_response": "响应异常",
  "next.site_unreachable": "检查站点地址和网络连接，然后重试。",
  "next.legacy_token_retired": "在站点的个人设置里创建 PAT（勾选 profile:read 和 usage:read），用它替换这个账户的凭据。",
  "next.pat_scope_denied": "重新创建 PAT，勾选 profile:read 和 usage:read，再替换这个账户的凭据。",
  "next.credential_invalid": "站点拒绝了这个凭据。确认凭据和用户 ID 无误，或在站点重新生成后替换。",
  "next.site_blocked": "站点返回了网页验证，而不是数据。插件不会绕过站点防护；请站点放行这些接口，或稍后重试。",
  "next.site_error": "站点暂时出错，稍后重试。",
  "next.bad_response": "站点返回了无法识别的数据。确认这是受支持的 new-api 站点后重试。",
  "needs.lastSuccess": "上次成功读取是 {when}，之后的余额和花费没有更新。",
  "needs.neverSucceeded": "CPA 启动以来还没有成功读到这个账户。",
  "needs.diagnostic": "诊断代码",
  "needs.retry": "重试",
  "needs.retrying": "正在重试",
  "unsupported.title": "当前版本还不支持自定义查询账户",
  "unsupported.text": "这个账户使用自定义查询。升级插件后即可读取。",

  "empty.title": "还没有账户",
  "empty.text": "添加站点账户后，这里会显示每个账户的余额和今日花费。",

  "loading.text": "正在读取账户…",

  "key.title": "需要 Management Key",
  "key.missing": "没有读到 CPA 管理页保存的 Management Key。",
  "key.rejected": "CPA 拒绝了这个 Management Key，请重新输入。",
  "key.note": "输入的 key 只保存在本页内存中，关闭页面后失效。",
  "key.label": "Management Key",
  "key.submit": "继续",

  "error.data_file_corrupt.title": "账户数据文件无法解析",
  "error.data_file_corrupt.text": "插件不会覆盖这个文件。检查文件内容或从备份恢复，然后刷新。",
  "error.data_file_newer.title": "账户数据文件来自更新版本的插件",
  "error.data_file_newer.text": "插件不会覆盖这个文件。升级插件后再打开。",
  "error.data_file_unavailable.title": "无法使用账户数据目录",
  "error.data_file_unavailable.text": "检查插件配置里的 data_dir：需要是 CPA 可以读写的绝对路径。",
  "error.path": "文件位置",
  "error.network.title": "连接不到 CPA 管理接口",
  "error.network.text": "确认 CPA 正在运行，然后重试。",
  "error.http.title": "读取账户失败",
  "error.http.text": "CPA 返回了 HTTP {status}。",
  "error.refreshFailed": "刷新失败，下面是上次读到的数据。",
  "error.retry": "重试",

  "time.justNow": "刚刚",
} as const;

export type MessageKey = keyof typeof zh;

const en: Record<MessageKey, string> = {
  "page.title": "Balances · NewAPI Meter",

  "state.ok": "Normal",
  "state.low": "Low balance",
  "state.lowShort": "Low",
  "state.partial": "Partial",
  "state.needs_action": "Needs action",

  "band.label": "Accounts",
  "band.scrollLeft": "Scroll accounts left",
  "band.scrollRight": "Scroll accounts right",
  "band.today": "Today {amount}",
  "band.readFailed": "Read failed",
  "band.lastSuccess": "Last read {when}",
  "band.neverSucceeded": "Never read",
  "band.balanceUnavailable": "Balance unavailable",
  "band.unsupported": "Unsupported source",
  "gauge.ratio": "Balance is {ratio}× the low-balance threshold {threshold}; the tick marks the threshold, a full bar is 4×",
  "gauge.noThreshold": "No low-balance threshold",
  "gauge.unknown": "Partial accounts are not checked against the threshold",

  "total.label": "All accounts",
  "total.today": "Today {amount}",
  "total.excludedOne": "{n} account not counted",
  "total.excludedMany": "{n} accounts not counted",
  "total.excludedHint": "Partial and needs-action accounts are left out of the total",

  "credential.pat": "PAT",
  "credential.legacy": "Legacy access token",
  "detail.siteVersion": "Site {version}",
  "detail.updated": "Updated {when}",
  "detail.lastAttempt": "Last tried {when}",
  "detail.refresh": "Refresh",
  "detail.refreshing": "Refreshing",
  "detail.refreshHint": "Query every account again, skipping the cache",

  "metric.balance": "Balance",
  "metric.threshold": "Threshold {amount}",
  "metric.belowThreshold": "Below threshold {amount}",
  "metric.noThreshold": "No threshold",
  "metric.thresholdSkipped": "Partial: threshold not checked",
  "metric.totalUsed": "Total used",
  "metric.todaySpend": "Today's spend",
  "metric.realtime": "Live RPM / TPM",
  "metric.realtimeSub": "Last 60 seconds",
  "metric.unavailable": "Unavailable",

  "partial.title": "Some data could not be read",
  "partial.item": "{part}: {reason}",
  "part./api/status": "Site settings (currency)",
  "part./api/user/self": "Balance and total used",
  "part./api/log/self/stat": "Today's spend and live RPM/TPM",
  "part./api/data/self": "Usage by model",

  "failure.site_unreachable": "Site unreachable",
  "failure.legacy_token_retired": "Legacy access token retired",
  "failure.pat_scope_denied": "PAT lacks permissions",
  "failure.credential_invalid": "Credential rejected",
  "failure.site_blocked": "Blocked by site protection",
  "failure.site_error": "Site error",
  "failure.bad_response": "Unexpected response",
  "next.site_unreachable": "Check the site address and network connection, then retry.",
  "next.legacy_token_retired": "Create a PAT in the site's personal settings (with profile:read and usage:read) and use it as this account's credential.",
  "next.pat_scope_denied": "Create a new PAT with profile:read and usage:read and use it as this account's credential.",
  "next.credential_invalid": "The site rejected this credential. Check the credential and user ID, or generate a new one on the site.",
  "next.site_blocked": "The site answered with a web challenge instead of data. The plugin does not bypass site protection; ask the site to allow these endpoints, or retry later.",
  "next.site_error": "The site is having trouble. Retry later.",
  "next.bad_response": "The site sent data the plugin does not recognize. Make sure it is a supported new-api site, then retry.",
  "needs.lastSuccess": "Last successful read was {when}; balance and spend have not updated since.",
  "needs.neverSucceeded": "This account has not been read successfully since CPA started.",
  "needs.diagnostic": "Diagnostic code",
  "needs.retry": "Retry",
  "needs.retrying": "Retrying",
  "unsupported.title": "This version does not support custom-query accounts yet",
  "unsupported.text": "This account uses a custom query. Update the plugin to read it.",

  "empty.title": "No accounts yet",
  "empty.text": "Once you add a site account, its balance and today's spend appear here.",

  "loading.text": "Loading accounts…",

  "key.title": "Management Key required",
  "key.missing": "No Management Key saved by the CPA management page was found.",
  "key.rejected": "CPA rejected this Management Key. Enter it again.",
  "key.note": "The key stays in this page's memory only and is gone when you close the page.",
  "key.label": "Management Key",
  "key.submit": "Continue",

  "error.data_file_corrupt.title": "The account data file cannot be read",
  "error.data_file_corrupt.text": "The plugin will not overwrite it. Check the file or restore a backup, then refresh.",
  "error.data_file_newer.title": "The account data file is from a newer plugin version",
  "error.data_file_newer.text": "The plugin will not overwrite it. Update the plugin, then open this page again.",
  "error.data_file_unavailable.title": "The account data directory is unavailable",
  "error.data_file_unavailable.text": "Check data_dir in the plugin configuration: it must be an absolute path CPA can read and write.",
  "error.path": "File",
  "error.network.title": "Cannot reach the CPA management API",
  "error.network.text": "Make sure CPA is running, then retry.",
  "error.http.title": "Failed to load accounts",
  "error.http.text": "CPA answered HTTP {status}.",
  "error.refreshFailed": "Refresh failed. Showing the last data read.",
  "error.retry": "Retry",

  "time.justNow": "just now",
};

export type Lang = "zh" | "en";

/** zh or en from navigator.language(s); anything undecidable is zh. */
export function detectLang(): Lang {
  const list = [navigator.language, ...(navigator.languages ?? [])];
  for (const tag of list) {
    const l = (tag ?? "").toLowerCase();
    if (l.startsWith("zh")) return "zh";
    if (l.startsWith("en")) return "en";
  }
  return "zh";
}

export const lang: Lang = detectLang();
export const locale = lang === "zh" ? "zh-CN" : "en-US";
const table: Record<MessageKey, string> = lang === "zh" ? zh : en;

export function t(key: MessageKey, vars?: Record<string, string | number>): string {
  const s = table[key];
  return vars ? s.replace(/\{(\w+)\}/g, (m, k: string) => (k in vars ? String(vars[k]) : m)) : s;
}

/** Look up a key built at runtime (endpoint or failure code); unknown keys fall back. */
export function tDynamic(key: string, fallback: string): string {
  return key in table ? table[key as MessageKey] : fallback;
}
