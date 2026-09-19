// ————————————————————————————————————————————————————————————————————————————
// uuid —— 随机 UUID 生成 —— 文件总结
//
// 对 github.com/google/uuid 的极薄封装,生成随机 UUID(v4):
// 36 个字符,格式 8-4-4-4-12,如
//
//	"6ba7b810-9dad-11d1-80b4-00c04fd430c8"
//
// 碰撞概率可忽略,常用于请求 id、幂等键、临时文件名等。
// ————————————————————————————————————————————————————————————————————————————
package utils

import "github.com/google/uuid"

// NewUuid returns an uuid string.
// 生成一个随机 UUID(v4)字符串。
func NewUuid() string {
	return uuid.New().String()
}
