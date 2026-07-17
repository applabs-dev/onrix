package setting

// Lemon Squeezy 一次性充值(topup)配置。值通过 OptionMap 从 DB 加载(见 model/option.go),
// 不直接读 env。对照 payment_creem.go,额外多一个 StoreId(LS 创建 checkout 必需)。
var LemonSqueezyApiKey = ""
var LemonSqueezyStoreId = ""
var LemonSqueezyProducts = "[]"
var LemonSqueezyTestMode = false
var LemonSqueezyWebhookSecret = ""
