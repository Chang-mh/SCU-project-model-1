package core

import "testing"

func TestSimHashStringMatchesClientAlgorithm(t *testing.T) {
	cases := map[string]string{
		"客户名称：四川示例科技有限公司\n联系人：张三\n报价：50万元": "9fa2c69b562580db",
		"hello world 123": "4579da5e102a8cdb",
		"手机号13800138000 API_KEY abcdefghijklmnop": "ffdd480d060bb877",
	}

	for text, want := range cases {
		if got := SimHashString(text); got != want {
			t.Fatalf("SimHashString(%q) = %s, want %s", text, got, want)
		}
	}
}
