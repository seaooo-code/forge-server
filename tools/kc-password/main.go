// Command kc-password 用小米 KeyCenter 解密数据库密码密文，明文写到标准输出。
//
// 它是 forge-server 的可选外部组件，通过 POSTGRES_PASSWORD_COMMAND 被调用；
// KeyCenter SDK 是公司内网私有模块，所以放在独立 module，不进入主 module 的依赖。
//
// 环境变量：
//
//	KEYCENTER_SID              必填，加解密使用的 SID
//	POSTGRES_PASSWORD_CIPHER   必填，KeyCenter 密文（base64 或 base64 urlsafe）
//	KEYCENTER_AGENT_HOST       可选，agent 地址，默认 127.0.0.1（容器里的 kc-agent sidecar）
//	KEYCENTER_AGENT_PORT       可选，agent 端口，默认 9988
package main

import (
	"fmt"
	"os"

	"git.n.xiaomi.com/keycenter/keycenter-sdk-go/keycenter"
)

func main() {
	sid := os.Getenv("KEYCENTER_SID")
	cipher := os.Getenv("POSTGRES_PASSWORD_CIPHER")
	if sid == "" || cipher == "" {
		fmt.Fprintln(os.Stderr, "KEYCENTER_SID and POSTGRES_PASSWORD_CIPHER are required")
		os.Exit(2)
	}
	if host := os.Getenv("KEYCENTER_AGENT_HOST"); host != "" {
		keycenter.CurrentHost = host
	}
	if port := os.Getenv("KEYCENTER_AGENT_PORT"); port != "" {
		keycenter.CurrentPort = port
	}

	// SDK 会用 fmt.Println 把诊断信息写到标准输出，调用期间改接到标准错误，保证标准输出只有密码。
	stdout := os.Stdout
	os.Stdout = os.Stderr
	password, err := keycenter.Decrypt(sid, cipher)
	os.Stdout = stdout
	if err != nil {
		fmt.Fprintf(os.Stderr, "keycenter decrypt failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintln(stdout, password)
}
