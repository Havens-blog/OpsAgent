package chat

import "testing"

func TestStripHallucinatedToolCall(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{
			"<tool_call>\n<function=list_cmdb_instances>\n<parameter=cloud_provider>volcengine</parameter>\n</tool_call>",
			"本次回答未能执行数据查询（相关数据源未接入或不可用），请确认数据源配置或稍后重试。",
		},
		{"<function=query_logs>", "本次回答未能执行数据查询（相关数据源未接入或不可用），请确认数据源配置或稍后重试。"},
		{"共 143 个 CDN 实例，本次返回前 50 个", "共 143 个 CDN 实例，本次返回前 50 个"},
	}
	for _, c := range cases {
		if got := stripHallucinatedToolCall(c.in); got != c.want {
			t.Errorf("strip(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
