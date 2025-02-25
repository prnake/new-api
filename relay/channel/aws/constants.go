package aws

var awsModelIDMap = map[string]string{
	"claude-instant-1.2":         "us.anthropic.claude-instant-v1",
	"claude-2.0":                 "us.anthropic.claude-v2",
	"claude-2.1":                 "us.anthropic.claude-v2:1",
	"claude-3-sonnet-20240229":   "us.anthropic.claude-3-sonnet-20240229-v1:0",
	"claude-3-opus-20240229":     "us.anthropic.claude-3-opus-20240229-v1:0",
	"claude-3-haiku-20240307":    "us.anthropic.claude-3-haiku-20240307-v1:0",
	"claude-3-5-sonnet-20240620": "us.anthropic.claude-3-5-sonnet-20240620-v1:0",
	"claude-3-5-sonnet-20241022": "us.anthropic.claude-3-5-sonnet-20241022-v2:0",
	"claude-3-5-haiku-20241022":  "us.anthropic.claude-3-5-haiku-20241022-v1:0",
    "claude-3-7-sonnet-20250219": "us.anthropic.claude-3-7-sonnet-20250219-v1:0",
}

var ChannelName = "aws"
