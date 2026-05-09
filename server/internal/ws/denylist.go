package ws

import "strings"

// chatDenylist is a tiny case-insensitive substring filter for the worst
// gambling/payment-related terms that contradict the "non-money" positioning.
// It's not a content moderation system — just a guardrail to keep stuff like
// "加微信送钱" out of broadcasts. For real moderation pair this with
// 微信 textcontentcheck on a sample of messages.
var chatDenylist = []string{
	"赌博", "博彩", "押注换钱", "加微", "加微信",
	"现金", "提现", "套现", "充值", "私聊换", "兑换码",
	"qq群", "微信群", "vx", "wechat", "telegram", "tg群",
	"色情", "黄网", "fuck",
}

// chatAllowed reports whether the given chat content is permitted to be
// broadcast. The check is intentionally cheap and conservative — false means
// "drop this message".
func chatAllowed(s string) bool {
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	for _, kw := range chatDenylist {
		if strings.Contains(lower, kw) {
			return false
		}
	}
	return true
}
