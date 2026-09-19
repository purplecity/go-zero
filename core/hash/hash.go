package hash

import (
	"crypto/md5"
	"encoding/hex"

	"github.com/spaolacci/murmur3"
)

// Hash returns the hash value of data.
// Hash 返回 64 位 murmur3 哈希值 —— 一个 0 ~ 2^64-1 的 uint64 整数。
// 其输出分布均匀,因此对任意正整数 N 取模即可得到 [0, N-1] 上
// 均匀且确定性的下标(一致性哈希按取模选节点正是基于这一点)。
func Hash(data []byte) uint64 {
	return murmur3.Sum64(data)
}

// Md5 returns the md5 bytes of data.
func Md5(data []byte) []byte {
	digest := md5.New()
	digest.Write(data)
	return digest.Sum(nil)
}

// Md5Hex returns the md5 hex string of data.
// This function is optimized for better performance than fmt.Sprintf.
func Md5Hex(data []byte) string {
	return hex.EncodeToString(Md5(data))
}
