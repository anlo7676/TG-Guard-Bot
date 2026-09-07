package domain

// BuiltinRule is the single source for matching metadata, menus and the web editor.
type BuiltinRule struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Score   int    `json:"score"`
	Action  string `json:"action"`
	Pattern string `json:"pattern,omitempty"`
	Example string `json:"example,omitempty"`
}

var BuiltinRules = []BuiltinRule{
	{Key: "ad_collection_jobs", Label: "收款码接单招揽", Pattern: "(?:收款码|收钱码|收款账户).{0,32}(?:来做|来接|接单|招人|日结|佣金|打钱爽快)|(?:[0-9一二三四五六七八九十]+分钟一单|担保公群).{0,40}(?:收款码|打钱爽快)", Example: "赌博料子没风险6分钟一单 有收款码的来做 好上手 有担保公群打钱爽快", Score: 60, Action: "delete"},
	{Key: "ad_launder_income", Label: "洗钱高收入诱导", Pattern: "(?:洗钱|跑分).{0,12}(?:一天|日入|日赚).{0,6}[0-9一二三四五六七八九十]+(?:千|万|百).{0,16}(?:担保|联系|来做|进群|包赚)", Example: "洗钱一天3千有担保群", Score: 60, Action: "delete"},
	{Key: "ad_sports_tips", Label: "足球红单引流", Score: 60, Action: "delete", Pattern: "(?:足球|篮球|体育).{0,16}(?:红单|推单|推荐单).{0,40}(?:交流群|领红包|加入|入群|@[a-z0-9_]{5,32})", Example: "足球红单推荐交流群.加入免费领红包 @losusnh9071bot"},
	{Key: "ad_bonus_bot", Label: "红包机器人引流", Score: 60, Action: "delete", Pattern: "(?:加入|进群|入群|领取|免费领).{0,16}(?:红包|福利|彩金).{0,32}@[a-z0-9_]{2,29}bot", Example: "加入免费领红包 @bonus9071bot"},
	{Key: "ad_luxury_jobs", Label: "豪车高薪招工", Score: 60, Action: "delete", Pattern: "(?:来帮我干活|跟我干|跟着我干|招人|招聘|不想上班).{0,64}(?:一个月|一月|月入|月赚).{0,12}(?:包提|喜提|提车|提奥迪|提宝马|提奔驰|[0-9一二三四五六七八九十]+万)", Example: "不想上班的来，来帮我干活，一个月包提奥迪A7"},
	{Key: "ad_paid_photos", Label: "拍招牌计件招揽", Score: 60, Action: "delete", Pattern: "(?:拍|收|采集).{0,8}(?:店铺|店面|门店).{0,8}(?:招牌|门头).{0,16}[0-9０-９]+\\s*(?:[0-9oＯ]|元|块|米|u|rmb)?\\s*(?:/|每|一)\\s*张", Example: "拍店铺招牌🛍️ 8o/张"},
	{Key: "ad_black_u_jobs", Label: "黑U项目招揽", Score: 60, Action: "delete", Pattern: "(?:来和我|跟我|一起|招人|招募|带你).{0,20}(?:做|赚|搞|洗|跑).{0,6}(?:黑\\s*u|黑钱|黑币)|(?:黑\\s*u|黑钱|黑币).{0,36}(?:招人|招募|私聊|联系|日结|一天.{0,12}(?:万|达不溜))", Example: "来和我一起做黑U，交易所的来，一天五个达不溜轻轻松松"},
	{Key: "url", Label: "外部 URL", Score: 20},
	{Key: "telegram_link", Label: "Telegram 链接", Score: 40},
	{Key: "contact", Label: "联系方式", Score: 25},
	{Key: "mention", Label: "用户名引流", Score: 20},
	{Key: "advertising", Label: "广告招揽", Score: 25},
	{Key: "gambling", Label: "博彩推广", Score: 35},
	{Key: "porn", Label: "色情推广", Score: 35},
	{Key: "crypto", Label: "币圈招揽", Score: 20},
	{Key: "caps", Label: "大量大写", Score: 15},
	{Key: "emoji", Label: "大量表情", Score: 15},
	{Key: "many_links", Label: "大量链接", Score: 25},
	{Key: "ad_profit", Label: "保本暴利承诺", Score: 60, Action: "delete", Pattern: "(?:稳赚不赔|稳赚包赔|包赚不赔|保本保收益|guaranteed profits?)", Example: "稳赚包赔，马上加入"},
	{Key: "ad_signals", Label: "投资带单招揽", Score: 60, Action: "delete", Pattern: "(?:(?:带单|喊单|合约导师|内幕股票).{0,48}(?:私聊|加微|入群|联系|收益翻倍|稳赚|日赚)|(?:私聊|加微|入群|联系|收益翻倍|稳赚|日赚).{0,48}(?:带单|喊单|合约导师|内幕股票))", Example: "合约导师带单，私聊入群"},
	{Key: "ad_gambling", Label: "博彩开户推广", Score: 60, Action: "delete", Pattern: "(?:(?:博彩|赌场|百家乐|六合彩|体育投注|时时彩).{0,48}(?:开户|送彩金|首充|充值返利|代理加盟|返点|包赢)|(?:开户|送彩金|首充|充值返利|代理加盟|返点|包赢).{0,48}(?:博彩|赌场|百家乐|六合彩|体育投注|时时彩))", Example: "百家乐开户送彩金"},
	{Key: "ad_adult", Label: "色情服务招揽", Score: 60, Action: "delete", Pattern: "(?:(?:裸聊|约炮|成人上门|成人视频|成人直播|同城小姐).{0,48}(?:私聊|联系|加微|预约|付费|包夜|进群|观看|体验)|(?:私聊|联系|加微|预约|付费|包夜|进群|观看|体验).{0,48}(?:裸聊|约炮|成人上门|成人视频|成人直播|同城小姐))", Example: "裸聊服务，私聊预约"},
	{Key: "ad_tasks", Label: "刷单返佣任务", Score: 60, Action: "delete", Pattern: "(?:(?:刷单|刷信誉|做单|垫付|抢单).{0,48}(?:返佣|返现|日结|佣金|先充值|垫资|高薪)|(?:返佣|返现|日结|佣金|先充值|垫资|高薪).{0,48}(?:刷单|刷信誉|做单|垫付|抢单))", Example: "刷单返佣，佣金日结"},
	{Key: "ad_loans", Label: "贷款中介广告", Score: 60, Action: "delete", Pattern: "(?:(?:贷款|借款|网贷|下款).{0,48}(?:无视征信|黑户可做|无需征信|包下款|秒到账|私聊|加微)|(?:无视征信|黑户可做|无需征信|包下款|秒到账|私聊|加微).{0,48}(?:贷款|借款|网贷|下款))", Example: "贷款无视征信，包下款"},
	{Key: "ad_cashout", Label: "信用额度套现", Score: 60, Action: "delete", Pattern: "(?:(?:花呗|白条|信用卡|信用额度).{0,48}(?:套现|提现秒到|代还提额|额度变现)|(?:套现|提现秒到|代还提额|额度变现).{0,48}(?:花呗|白条|信用卡|信用额度))", Example: "花呗套现，提现秒到"},
	{Key: "ad_otc", Label: "虚拟币买卖招揽", Score: 60, Action: "delete", Pattern: "(?:(?:收u|出u|换u|买u|卖u|usdt兑换|usdt回收).{0,48}(?:私聊|联系|加微|汇率|量大|秒到|长期)|(?:私聊|联系|加微|汇率|量大|秒到|长期).{0,48}(?:收u|出u|换u|买u|卖u|usdt兑换|usdt回收))", Example: "长期收U，量大私聊"},
	{Key: "ad_airdrop", Label: "空投钱包诱导", Score: 60, Action: "delete", Pattern: "(?:(?:空投|airdrop).{0,48}(?:连接钱包|授权钱包|助记词|私钥|connect your wallet|wallet verification)|(?:连接钱包|授权钱包|助记词|私钥|connect your wallet|wallet verification).{0,48}(?:空投|airdrop))", Example: "领取空投，请连接钱包授权"},
	{Key: "ad_groups", Label: "外部群引流", Score: 60, Action: "delete", Pattern: "(?:(?:进群|入群|加群|加入频道|福利群).{0,48}(?:t\\.me/|telegram\\.me/|加微|私聊领取|扫码|免费福利)|(?:t\\.me/|telegram\\.me/|加微|私聊领取|扫码|免费福利).{0,48}(?:进群|入群|加群|加入频道|福利群))", Example: "加入福利群 t.me/freebonus"},
	{Key: "ad_bulk", Label: "群发推广服务", Score: 60, Action: "delete", Pattern: "(?:(?:群发|群推|私信轰炸|精准引流|全网推广).{0,48}(?:接单|代发|价格|报价|联系|私聊|日发|万条)|(?:接单|代发|价格|报价|联系|私聊|日发|万条).{0,48}(?:群发|群推|私信轰炸|精准引流|全网推广))", Example: "群发推广接单，私聊报价"},
	{Key: "ad_accounts", Label: "社交账号买卖", Score: 60, Action: "delete", Pattern: "(?:(?:tg号|telegram账号|微信号|推特号|discord账号|脸书号|facebook账号).{0,48}(?:出售|批发|现货|收购|购买|低价)|(?:出售|批发|现货|收购|购买|低价).{0,48}(?:tg号|telegram账号|微信号|推特号|discord账号|脸书号|facebook账号))", Example: "TG号批发，长期现货"},
	{Key: "ad_sms", Label: "接码养号服务", Score: 60, Action: "delete", Pattern: "(?:(?:接码|养号|实名号|解封服务).{0,48}(?:平台出售|批发|现货|价格|私聊|联系|代办)|(?:平台出售|批发|现货|价格|私聊|联系|代办).{0,48}(?:接码|养号|实名号|解封服务))", Example: "接码养号，联系批发"},
	{Key: "ad_documents", Label: "证件文凭代办", Score: 60, Action: "delete", Pattern: "(?:(?:身份证|驾驶证|毕业证|学历证|学位证).{0,48}(?:办假证|高仿|代办包过|无需考试|保真可查|私聊办理)|(?:办假证|高仿|代办包过|无需考试|保真可查|私聊办理).{0,48}(?:身份证|驾驶证|毕业证|学历证|学位证))", Example: "毕业证代办包过，无需考试"},
	{Key: "ad_invoices", Label: "发票代开广告", Score: 60, Action: "delete", Pattern: "(?:(?:代开发票|发票代开|代开增值税).{0,48}(?:联系|私聊|点位|税点|保真|全国|加微)|(?:联系|私聊|点位|税点|保真|全国|加微).{0,48}(?:代开发票|发票代开|代开增值税))", Example: "全国代开发票，私聊税点"},
	{Key: "ad_data", Label: "个人资料交易", Score: 60, Action: "delete", Pattern: "(?:(?:个人信息|客户资料|手机号码库|精准客户数据|公民信息).{0,48}(?:出售|售卖|批发|一手资源|私聊交易)|(?:出售|售卖|批发|一手资源|私聊交易).{0,48}(?:个人信息|客户资料|手机号码库|精准客户数据|公民信息))", Example: "客户资料出售，一手资源"},
	{Key: "ad_followers", Label: "刷粉刷量服务", Score: 60, Action: "delete", Pattern: "(?:(?:刷粉|买粉|刷播放|刷赞|刷评论|机器人粉丝).{0,48}(?:接单|报价|价格|私聊|联系|千粉|万粉)|(?:接单|报价|价格|私聊|联系|千粉|万粉).{0,48}(?:刷粉|买粉|刷播放|刷赞|刷评论|机器人粉丝))", Example: "刷粉刷赞接单，私聊报价"},
	{Key: "ad_proxy", Label: "代理节点销售", Score: 60, Action: "delete", Pattern: "(?:(?:机场节点|vpn节点|专线节点|翻墙套餐).{0,48}(?:限时特价|优惠码|购买链接|月付[0-9]+|私聊购买)|(?:限时特价|优惠码|购买链接|月付[0-9]+|私聊购买).{0,48}(?:机场节点|vpn节点|专线节点|翻墙套餐))", Example: "机场节点限时特价，私聊购买"},
	{Key: "ad_crack", Label: "破解软件销售", Score: 60, Action: "delete", Pattern: "(?:(?:破解版|破解软件|盗版软件|永久激活).{0,48}(?:出售|低价购买|私聊购买|批发|代激活)|(?:出售|低价购买|私聊购买|批发|代激活).{0,48}(?:破解版|破解软件|盗版软件|永久激活))", Example: "破解软件低价购买，永久激活"},
	{Key: "ad_shopping", Label: "购物返利推广", Score: 60, Action: "delete", Pattern: "(?:(?:内部优惠券|购物返利|淘宝返佣|拼多多助力).{0,48}(?:加群|加微|私聊|扫码领取|推广链接)|(?:加群|加微|私聊|扫码领取|推广链接).{0,48}(?:内部优惠券|购物返利|淘宝返佣|拼多多助力))", Example: "内部优惠券加群领取"},
	{Key: "ad_jobs", Label: "高薪招募诱导", Score: 60, Action: "delete", Pattern: "(?:(?:兼职|招聘|招募).{0,48}(?:零门槛日赚|无门槛日赚|躺赚|日入[0-9]{3,}|先交押金|先交保证金)|(?:零门槛日赚|无门槛日赚|躺赚|日入[0-9]{3,}|先交押金|先交保证金).{0,48}(?:兼职|招聘|招募))", Example: "招聘兼职，零门槛日赚500"},
	{Key: "ad_english_invest", Label: "英文投资诈骗招揽", Score: 60, Action: "delete", Pattern: "(?:(?:crypto investment|forex signals|investment plan).{0,48}(?:guaranteed|double your|daily returns|dm me|contact me)|(?:guaranteed|double your|daily returns|dm me|contact me).{0,48}(?:crypto investment|forex signals|investment plan))", Example: "Crypto investment plan, guaranteed daily returns"},
	{Key: "ad_english_giveaway", Label: "英文赠币诱导", Score: 60, Action: "delete", Pattern: "(?:(?:send (?:btc|eth|usdt)|send your crypto).{0,48}(?:get double|double back|receive twice|giveaway)|(?:get double|double back|receive twice|giveaway).{0,48}(?:send (?:btc|eth|usdt)|send your crypto))", Example: "Send BTC to receive twice in our giveaway"},
	{Key: "ad_laundering", Label: "跑分洗钱招募", Score: 60, Action: "delete", Pattern: "(?:(?:跑分|洗钱|银行卡四件套|代收代付).{0,48}(?:招募|招人|佣金|日结|出租|收购|私聊)|(?:招募|招人|佣金|日结|出租|收购|私聊).{0,48}(?:跑分|洗钱|银行卡四件套|代收代付))", Example: "跑分招募，佣金日结"},
	{Key: "ad_recharge", Label: "低价代充广告", Score: 60, Action: "delete", Pattern: "(?:(?:话费代充|点卡代充|会员代充|游戏代充|代充会员).{0,48}(?:低价|折扣|联系|私聊|接单|批发)|(?:低价|折扣|联系|私聊|接单|批发).{0,48}(?:话费代充|点卡代充|会员代充|游戏代充|代充会员))", Example: "游戏代充低价接单"},
}

func FindBuiltinRule(key string) (BuiltinRule, bool) {
	for _, r := range BuiltinRules {
		if r.Key == key {
			return r, true
		}
	}
	return BuiltinRule{}, false
}
func EffectiveRule(s Settings, key string) RuleSetting {
	if r, ok := s.Rules[key]; ok {
		return r
	}
	if r, ok := FindBuiltinRule(key); ok {
		return RuleSetting{Enabled: true, Score: r.Score, Action: r.Action}
	}
	return RuleSetting{}
}
