// ————————————————————————————————————————————————————————————————————————————
// version —— 版本号比较 —— 文件总结
//
// CompareVersions(v1, op, v2) 判断 "v1 op v2" 是否成立,
// op 支持 =、==、<、>、<=、>=。
//
// compare 的比较规则:
//  1. 预处理:去掉 V/v 前缀;把 "-" 替换成 "."
//     (支持日期式版本 "v2023-01-01" → "2023.01.01");
//  2. 按 "." 切分逐段转成整数,逐段比较,先分出胜负即返回;
//  3. 公共段全部相等时,段数多者更大:"1.2" < "1.2.1"。
//
// 注意:非数字段会被静默解析为 0(strsToInts 忽略错误),
// 如 "1.x" 与 "1.0" 相等 —— 只适用于可控的内部版本号场景。
// ————————————————————————————————————————————————————————————————————————————
package utils

import (
	"cmp"
	"strconv"
	"strings"

	"github.com/zeromicro/go-zero/core/stringx"
)

// replacer 版本号预处理器:去 V/v 前缀,日期分隔符归一为 "."。
var replacer = stringx.NewReplacer(map[string]string{
	"V": "",
	"v": "",
	"-": ".",
})

// CompareVersions returns true if the first field and the third field are equal, otherwise false.
// (英文注释为历史遗留,准确语义:)判断版本关系表达式是否成立:
// 例如 CompareVersions("v1.2", "<", "1.10") → true。
// 不支持的操作符一律返回 false。
func CompareVersions(v1, op, v2 string) bool {
	result := compare(v1, v2)
	switch op {
	case "=", "==":
		return result == 0
	case "<":
		return result == -1
	case ">":
		return result == 1
	case "<=":
		return result == -1 || result == 0
	case ">=":
		return result == 0 || result == 1
	}

	return false
}

// return -1 if v1<v2, 0 if they are equal, and 1 if v1>v2
// 逐段比较两个版本号:v1<v2 返回 -1,相等返回 0,v1>v2 返回 1。
func compare(v1, v2 string) int {
	// 预处理:去前缀、归一分隔符,再按 "." 切段转整数。
	v1, v2 = replacer.Replace(v1), replacer.Replace(v2)
	fields1, fields2 := strings.Split(v1, "."), strings.Split(v2, ".")
	ver1, ver2 := strsToInts(fields1), strsToInts(fields2)
	ver1len, ver2len := len(ver1), len(ver2)
	shorter := min(ver1len, ver2len)

	// 逐段比较:从主版本开始,第一处不同即分出结果。
	for i := 0; i < shorter; i++ {
		if ver1[i] == ver2[i] {
			continue
		} else if ver1[i] < ver2[i] {
			return -1
		} else {
			return 1
		}
	}
	// 公共段全相等:段数多者版本更大("1.2" < "1.2.1")。
	return cmp.Compare(ver1len, ver2len)
}

// strsToInts 把各段转成 int64;
// 解析失败(非数字段)静默按 0 处理,不做严格校验。
func strsToInts(strs []string) []int64 {
	if len(strs) == 0 {
		return nil
	}

	ret := make([]int64, 0, len(strs))
	for _, str := range strs {
		i, _ := strconv.ParseInt(str, 10, 64)
		ret = append(ret, i)
	}

	return ret
}
