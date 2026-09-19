// ————————————————————————————————————————————————————————————————————————————
// replacer —— 基于 AC 自动机的多关键词替换器 —— 文件总结
//
// 一、用途
//
//	给定映射表 {"关键词A": "替身1", "关键词B": "替身2"...},
//	Replace(text) 把文本中出现的所有关键词替换为对应替身。
//	替换是"最长优先":同一位置命中多个关键词时替换最长那个,
//	如词典 {"ab": "X", "abc": "Y"} 对 "abc" 替换成 "Y" 而非 "Xab"。
//
// 二、为什么要 replaceTimes=2(最多两遍)?
//
//	替换结果本身可能又拼出一个新关键词,产生连锁反应,例如
//	词典 {"ab": "b"},文本 "aab":
//	  第一遍:找到 "ab"@[1,3) → 替换为 "b" → 得到 "ab"(又是关键词!)
//	  第二遍:找到 "ab"@[0,2) → 替换为 "b" → "b",稳定。
//	逐遍替换直到无命中,但为防无限循环最多做 2 遍。
//
// 三、实现
//
//	复用 node.go 的 AC 自动机:NewReplacer 时把所有 key add 进 Trie
//	并 build;Replace 时 find 出全部命中区间,按 start 升序、
//	stop 降序(同位置最长优先)排序,跳过与已替换区间重叠的部分,
//	拼接输出。
//
// ————————————————————————————————————————————————————————————————————————————
package stringx

import (
	"sort"
	"strings"
)

// replace more than once to avoid overlapped keywords after replace.
// only try 2 times to avoid too many or infinite loops.
// 替换后可能拼接出新关键词,需要再替换一遍;最多 2 遍防无限循环。
const replaceTimes = 2

type (
	// Replacer interface wraps the Replace method.
	// Replacer 多关键词替换器接口。
	Replacer interface {
		// Replace 把 text 中的所有关键词替换为对应替身。
		Replace(text string) string
	}

	// replacer 实现:内嵌 AC 自动机节点 + 替换映射表。
	replacer struct {
		*node
		// mapping 关键词 → 替身(键即 Trie 里的关键词)。
		mapping map[string]string
	}
)

// NewReplacer returns a Replacer.
// 构建替换器:所有 key 插入 Trie,再 build 出 fail 指针。
// 只需构建一次,之后 Replace 可并发调用(Trie 构建后只读)。
func NewReplacer(mapping map[string]string) Replacer {
	rep := &replacer{
		node:    new(node),
		mapping: mapping,
	}
	for k := range mapping {
		rep.add(k)
	}
	rep.build()

	return rep
}

// Replace replaces text with given substitutes.
// 执行替换:第一遍有命中才值得做第二遍(连锁替换),
// 没命中立即返回,常见路径零浪费。
func (r *replacer) Replace(text string) string {
	for i := 0; i < replaceTimes; i++ {
		var replaced bool
		if text, replaced = r.doReplace(text); !replaced {
			return text
		}
	}

	return text
}

// doReplace 单遍替换:返回新文本以及本遍是否发生过替换。
func (r *replacer) doReplace(text string) (string, bool) {
	chars := []rune(text)
	scopes := r.find(chars)
	if len(scopes) == 0 {
		return text, false
	}

	// 排序:start 升序;同 start 时 stop 降序 —— 即同一位置
	// 命中多个关键词时,最长者优先替换。
	sort.Slice(scopes, func(i, j int) bool {
		if scopes[i].start < scopes[j].start {
			return true
		}
		if scopes[i].start == scopes[j].start {
			return scopes[i].stop > scopes[j].stop
		}
		return false
	})

	// 按序拼接:index 之前是未命中的原文,命中区间替换为 mapping 值;
	// start < index 说明该区间与已替换区间重叠,直接跳过。
	var buf strings.Builder
	var index int
	for i := 0; i < len(scopes); i++ {
		scp := &scopes[i]
		if scp.start < index {
			continue
		}

		buf.WriteString(string(chars[index:scp.start]))
		buf.WriteString(r.mapping[string(chars[scp.start:scp.stop])])
		index = scp.stop
	}
	// 收尾:最后一个命中区间之后的剩余原文。
	if index < len(chars) {
		buf.WriteString(string(chars[index:]))
	}

	return buf.String(), true
}
