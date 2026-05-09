package ws

import "testing"

func TestChatAllowed(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"👍", true},
		{"😂😱🔥", true},
		{"good play", true},
		{"", false},
		{"加微信123", false},
		{"私聊换钱", false}, // contains the denylisted "私聊换" substring
		{"FUCK", false},
		{"我有微信群", false},
	}
	for _, c := range cases {
		if got := chatAllowed(c.in); got != c.want {
			t.Errorf("chatAllowed(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
