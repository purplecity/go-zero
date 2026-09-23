// ————————————————————————————————————————————————————————————————————————————
// netx —— 网络工具 —— 文件总结
//
// InternalIp:遍历本机网卡,返回第一个"可用、非回环、IPv4"的
// 内网 IP。服务注册(zrpc/etcd 注册)、日志标识等场景用它确定
// 本机对外的内网地址。
//
// 筛选规则(按序跳过):
//   - 网卡未 UP(eth down);
//   - 回环网卡(127.0.0.1);
//   - 非 IPv4 地址(To4() == nil,即 IPv6);
//   - 回环地址(IP.IsLoopback)。
//
// 找不到返回空串,由调用方兜底。
// ————————————————————————————————————————————————————————————————————————————
package netx

import "net"

// InternalIp returns an internal ip.
// 返回本机内网 IPv4 地址;枚举全部网卡失败或找不到时返回空串。
func InternalIp() string {
	infs, err := net.Interfaces()
	if err != nil {
		return ""
	}

	// 逐个网卡逐个地址筛选。
	for _, inf := range infs {
		if isEthDown(inf.Flags) || isLoopback(inf.Flags) {
			continue
		}

		addrs, err := inf.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					return ipnet.IP.String()
				}
			}
		}
	}

	return ""
}

// isEthDown 判断网卡是否未启用(没有 UP 标志)。
func isEthDown(f net.Flags) bool {
	return f&net.FlagUp != net.FlagUp
}

// isLoopback 判断网卡是否为回环设备(lo)。
func isLoopback(f net.Flags) bool {
	return f&net.FlagLoopback == net.FlagLoopback
}
