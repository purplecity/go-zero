// ————————————————————————————————————————————————————————————————————————————
// prometheus/config —— Prometheus 配置 —— 文件总结
//
// 来自服务配置文件的 prometheus 段:
//
//	Host 监听地址(空 = 不启用,agent 直接返回);
//	Port 监听端口(默认 9101);
//	Path 指标抓取路径(默认 /metrics)。
//
// ————————————————————————————————————————————————————————————————————————————
package prometheus

// A Config is a prometheus config.
// Prometheus 抓取 server 配置。
type Config struct {
	// Host 监听地址,如 0.0.0.0;为空表示不启用。
	Host string `json:",optional"`
	// Port 监听端口,默认 9101。
	Port int `json:",default=9101"`
	// Path 指标暴露路径,默认 /metrics。
	Path string `json:",default=/metrics"`
}
