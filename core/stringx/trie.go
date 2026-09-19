// ————————————————————————————————————————————————————————————————————————————
// trie —— 基于 AC 自动机的敏感词过滤 Trie —— 文件总结
//
// 一、用途
//
//	内容安全场景:给定敏感词列表,Filter(text) 返回:
//	  sentence  —— 敏感词被遮罩符替换后的文本(默认换成 '*');
//	  keywords  —— 命中的敏感词列表(去重);
//	  found     —— 是否命中。
//	FindKeywords(text) 只查不换,返回命中的敏感词列表。
//
// 二、与 replacer 的分工
//
//	两者共用 node.go 的 AC 自动机内核:
//	replacer 做"替换成指定字符串",trie 做"统一遮罩 + 收集命中词"。
//	遮罩是等长逐字符覆盖,因此命中区间即使重叠也无所谓
//	(重复覆盖 '*' 是幂等的),不做区间去重。
//
// ————————————————————————————————————————————————————————————————————————————
package stringx

import "github.com/zeromicro/go-zero/core/lang"

// defaultMask 默认遮罩符 '*'。
const defaultMask = '*'

type (
	// TrieOption defines the method to customize a Trie.
	// Trie 选项(函数式选项模式),目前支持 WithMask。
	TrieOption func(trie *trieNode)

	// A Trie is a tree implementation that used to find elements rapidly.
	// Trie 敏感词过滤接口,基于 AC 自动机,多关键词一次扫描全命中。
	Trie interface {
		// Filter 返回遮罩后的文本、命中的关键词列表、是否命中。
		Filter(text string) (string, []string, bool)
		// FindKeywords 只查不换,返回命中的关键词列表。
		FindKeywords(text string) []string
	}

	// trieNode 在 AC 节点基础上增加遮罩符配置。
	trieNode struct {
		node
		// mask 命中字符的替换符,默认 '*'。
		mask rune
	}

	// scope 一次命中的区间[rune 下标,左闭右开),定义在 trie.go 供
	// node.go/replacer.go 共用。
	scope struct {
		start int
		stop  int
	}
)

// NewTrie returns a Trie.
// 构建敏感词过滤 Trie:应用选项 → 逐个插入敏感词 → build fail 指针。
// 构建后 Trie 只读,Filter/FindKeywords 可并发调用。
func NewTrie(words []string, opts ...TrieOption) Trie {
	n := new(trieNode)

	for _, opt := range opts {
		opt(n)
	}
	// 未配置遮罩符时用默认 '*'。
	if n.mask == 0 {
		n.mask = defaultMask
	}
	for _, word := range words {
		n.add(word)
	}

	n.build()

	return n
}

// Filter 返回遮罩后的文本、命中关键词与是否命中。
func (n *trieNode) Filter(text string) (sentence string, keywords []string, found bool) {
	chars := []rune(text)
	if len(chars) == 0 {
		return text, nil, false
	}

	// 一遍扫描拿到全部命中区间。
	scopes := n.find(chars)
	keywords = n.collectKeywords(chars, scopes)

	for _, match := range scopes {
		// we don't care about overlaps, not bringing a performance improvement
		// 区间重叠也无需处理:遮罩是逐字符覆盖,重复覆盖幂等。
		n.replaceWithAsterisk(chars, match.start, match.stop)
	}

	return string(chars), keywords, len(keywords) > 0
}

// FindKeywords 只查不换,返回命中的关键词(已去重)。
func (n *trieNode) FindKeywords(text string) []string {
	chars := []rune(text)
	if len(chars) == 0 {
		return nil
	}

	scopes := n.find(chars)
	return n.collectKeywords(chars, scopes)
}

// collectKeywords 把命中区间翻译成关键词并去重:
// 同一个词可能在文本中出现多次,或以不同区间重叠命中。
func (n *trieNode) collectKeywords(chars []rune, scopes []scope) []string {
	set := make(map[string]lang.PlaceholderType)
	for _, v := range scopes {
		set[string(chars[v.start:v.stop])] = lang.Placeholder
	}

	var i int
	keywords := make([]string, len(set))
	for k := range set {
		keywords[i] = k
		i++
	}

	return keywords
}

// replaceWithAsterisk 把区间 [start, stop) 内的字符统一替换为遮罩符。
func (n *trieNode) replaceWithAsterisk(chars []rune, start, stop int) {
	for i := start; i < stop; i++ {
		chars[i] = n.mask
	}
}

// WithMask customizes a Trie with keywords masked as given mask char.
// 选项:自定义遮罩符,如 WithMask('#')。
func WithMask(mask rune) TrieOption {
	return func(n *trieNode) {
		n.mask = mask
	}
}
