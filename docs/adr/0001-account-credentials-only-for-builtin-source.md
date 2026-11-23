# 内置数据来源只接受账户凭据

new-api 的账户级数据（余额、花费、按模型与小时的用量）只能用账户凭据读取，API Key 只能看到单个 key 的额度；现有插件 balance-query 已覆盖 key 级查询。因此内置 new-api 数据来源不提供 API Key 模式，与其互补；需要 key 级或非 new-api 服务的余额时使用自定义查询。
