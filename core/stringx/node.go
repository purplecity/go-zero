// ————————————————————————————————————————————————————————————————————————————
// node —— 字典树(Trie)与 AC 自动机(Aho-Corasick)内核 —— 图解总结
//
// 这是 stringx 包的核心引擎,replacer(多关键词替换)和
// trie(敏感词过滤)都构建在它之上。
//
// 一、什么是字典树(Trie)?
//
//	一棵按「字符逐层分叉」的多叉树:
//	  - 每条边代表一个字符,从根走到某个节点,沿途字符拼起来
//	    恰好是一个字符串前缀;
//	  - 把关键词的最后一个字符节点标记为「词尾」(end=true),
//	    这个词就存进了树里;
//	  - 关键特性:公共前缀只存一份 —— "he" 和 "her" 共用
//	    h→e 这条路径,省内存且查询快。
//
//	例:把 {"he", "her", "she"} 三个词插入字典树
//	(* 表示词尾节点,end=true):
//
//	          (root)
//	         /      \
//	        h        s
//	        |        |
//	        e*       h
//	        |        |
//	        r*       e*
//	     (her)     (she)
//
//	左链 h→e*→r* 存了两个词:"he"(中间的 e*)和 "her"(底部的 r*),
//	注意 "her" 走的是 h→e→r,和右链没关系;
//	右链 s→h→e* 存了 "she"。查找就是顺着字符往下走:能走到底且
//	结尾带 * 就是存在;中途断掉(如 "hex" 走到 e 后无 x 分支)则不存在。
//
// 二、fail 指针到底干啥用?(AC 自动机的灵魂)
//
//	先记住扫描铁律:文本指针只前进、从不回退。于是有两个问题必须解决:
//	① 匹配断裂时,下一步从哪继续?② 一个位置同时结束多个词时,
//	怎么把它们都收齐?fail 指针就是预先算好的答案。
//
//	定义:节点的 fail 指向「自己对应字符串的最长真后缀,
//	且该后缀也是树中某条路径」的节点。
//
//	拆解「最长真后缀」这三个词(以 "she" 为例):
//
//	后缀     从字符串尾部连续剪下来的任意一段,都以最后一个
//	         字符结尾。"she" 的后缀:"she"、"he"、"e"。
//	真       严格比自己短 —— 把整串本身从后缀集合里抠掉
//	         (数学里「真子集」的同一个「真」;fail 跳到
//	         自己身上等于没跳,所以必须排除整串)。
//	         "she" 的真后缀:"he"、"e"。
//	最长     真后缀里长度最大的那个。"she" 的最长真后缀是 "he"
//	         (2 字符,胜过 "e" 的 1 字符)。
//
//	fail 的完整查找过程 = 把真后缀从长到短挨个检查,
//	第一个「是树中路径」的就是去处,全都不行就指向 root:
//
//	"she":  "he"(2字符,✓ 是树中路径)→ fail = he 节点
//	"her":  "er"(2字符,✗)→ "r"(1字符,✗)→ fail = root
//
//	为什么优先最长?后缀越长,保留的已匹配上下文越多,越不漏词:
//	跳到 "he" 还能接着看后面的字符;跳到 "e" 就把 "h" 的信息丢了。
//
//	用途一(断裂续命,文本不回头)。词典 {"she", "he"},文本 "shhe":
//
//	i=0 's':  root→s
//	i=1 'h':  s→sh
//	i=2 'h':  sh 没有 'h' 分支,断了!
//	          fail 跳转:sh 的 fail 是 h(因为 "sh" 的后缀 "h"
//	          恰是树中路径),h 节点正好能接上 'h' → 站到 h 节点。
//	          相当于把刚扫过的这个 'h' 重新当作一个新词的开头,
//	          文本一个字符都没回退。
//	i=3 'e':  h→he*,he 是词尾 → 命中 "he" [2,4)
//
//	若没有 fail,断裂后只能回 root 从头重新试探(慢且繁琐);
//	fail 一步跳到「等效续接点」。
//
//	用途二(一个位置收齐所有词,不漏)。文本 "she":
//
//	i=2 'e' 走到 she 节点:she 是词尾 → 命中 "she";
//	沿 fail 链继续:she→he,he 也是词尾 → "he" 同时命中!
//	("she" 的结尾必然也是 "he" 的结尾,不走 fail 链就漏掉 "he")
//
//	上例建好后的 fail 指针全表:
//
//	节点    fail→   原因
//	h       root    "h" 没有非空真后缀
//	s       root    同上
//	he      root    "he" 的后缀 "e" 不是树中任何路径的开头
//	sh      h       "sh" 的后缀 "h" 恰是树中路径 ✓
//	her     root    "her" 的后缀 "er"/"r" 都不匹配
//	she     he      "she" 的后缀 "he" 恰是树中路径 ✓(关键!)
//
// 三、find 扫描示例:文本 "usher"
//
//	i  字符  动作
//	0  u     root 无 u 分支,留在 root
//	1  s     root→s。s 非词尾;s.fail=root 也非词尾,无命中。
//	2  h     s→sh。非词尾;fail 链 h(路径 "sh")→h(第一层)都非词尾,无命中。
//	3  e     sh→she:she 是词尾 → 命中 "she" 区间 [1,4);
//	        沿 fail 链 she→he:he 也是词尾 → 命中 "he" 区间 [2,4)
//	4  r     she 无 r 分支 → fail 回退到 e(路径 "he"):he 有 'r'
//	        分支 → 走到 r(路径 "her"):her 是词尾 → 命中 "her"[2,5)
//
//	整个文本只扫了一遍:i=3 一个位置同时揪出两个词,
//	i=4 靠 fail 链救回 "her" —— 文本指针从头到尾没有回退过一步。
//	命中区间记成 scope{start, stop}(rune 下标,左闭右开):
//	start = i+1-节点depth,stop = i+1
//	(depth 即「从根走到该节点用了几个字符」,正好是词长)。
//
// 四、复杂度
//
//	构建:O(所有关键词的字符总数);查找:O(文本长 + 命中数),
//	与关键词数量无关 —— 词典里 1 个词和 1 万个词,扫描速度一样。
//
// 五、字段含义
//
//	children 子节点表(rune → 子节点,按字符转移);
//	fail     失配跳转指针(build 后形成自动机);
//	depth    当前节点在树中的字符深度,配合 i 反推匹配起点;
//	end      是否是某个关键词的结尾(完整词标记)。
//
// 六、使用流程
//
//	add(逐个插入关键词) → build(BFS 建 fail 指针) → find(扫文本收集命中区间)
//	约定:必须先 build 再 find,且 build 之后不能再 add。
//
// ————————————————————————————————————————————————————————————————————————————
package stringx

// node 是 AC 自动机的树节点(内部类型,不对外暴露)。
type node struct {
	// children 按 rune 的子节点转移表(nil 表示叶子)。
	children map[rune]*node
	// fail 失配指针:指向当前匹配串的最长真后缀对应的节点。
	fail *node
	// depth 从根到本节点的字符数,用于反推命中的起始下标。
	depth int
	// end 是否为某个关键词的最后一个字符。
	end bool
}

// add 向 Trie 插入一个关键词(按 rune 逐层建节点)。
func (n *node) add(word string) {
	chars := []rune(word)
	if len(chars) == 0 {
		return
	}

	nd := n
	for i, char := range chars {
		if nd.children == nil {
			// 第一个子节点:就地建表,顺带记录深度。
			child := new(node)
			child.depth = i + 1
			nd.children = map[rune]*node{char: child}
			nd = child
		} else if child, ok := nd.children[char]; ok {
			// 已有该字符的分支:复用(Trie 的公共前缀共享)。
			nd = child
		} else {
			// 新建分支。
			child := new(node)
			child.depth = i + 1
			nd.children[char] = child
			nd = child
		}
	}

	// 最后一个字符节点标记为完整关键词结尾。
	nd.end = true
}

// build 用 BFS(层序遍历)为所有节点构建 fail 指针,
// 把 Trie 变成 AC 自动机。必须在全部 add 之后调用一次。
//
// 一、为什么外层 for len(nodes) > 0 不会死循环?
//
//	nodes 是一个 BFS 队列,收支是平的:
//	  出队:每轮循环恰好移除 1 个节点(nodes[0] 取走,nodes[1:] 截掉);
//	  入队:每个节点只在「它的父节点出队」时被放入一次,
//	        Trie 是树(无环),每节点仅一个父节点 → 一生只入队一次。
//	入队总数 = 节点总数(有限),队列必然耗尽。这是所有 BFS 的标准写法。
//
// 二、为什么必须是 BFS(按层处理),顺序不能乱?
//
//	算 child.fail 的过程依赖「父节点 nd 的 fail 已经算好」
//	(内层从 nd.fail 沿链向上找)。BFS 按深度从小到大出队:
//	处理第 k 层时,第 k-1 层的 fail 刚全部算完,依赖永远就绪。
//
// 三、内层 for cur != nil 也不会死循环:
//
//	fail 永远指向更浅的节点(真后缀必然比原串短),
//	cur = cur.fail 每步深度严格减 1,最多 depth 步走到根;
//	只有根的 fail 是 nil,走到根即退出。
//
// 四、为什么找的是 cur.fail.children[key] 而不是 cur.children[key]?
//
//	child 对应串 = "nd 的串 + key",它的候选后缀要从「去掉首字符」
//	开始试:"nd 的串"的 fail + key 正好是「去首字符后再加 key」,
//	即最长候选;链上继续向上 = 后缀从长到短依次尝试。
//
// 五、队列变化全过程(词典 {"he", "her", "she"}):
//
//	节点一律用单字符称呼(就是 children 里的 key)。
//	树里有两个 'h'、两个 'e',按出队顺序对号入座即可。
//
//	初始:队列 [h, s],h.fail = s.fail = root   ← 第一层,fail 必是根
//	出队 h → 孩子:字符 'e',下面记作 e:
//	    cur=h,cur.fail=root,root 无 'e' → cur=root,cur.fail=nil
//	    → e.fail = root;入队。队列:[s, e]
//	出队 s → 孩子:字符 'h',下面记作 h(第一层已有一个 h,这是第二个):
//	    cur=s,cur.fail=root,root 有 'h' → h.fail = h ✓(指向第一层那个 h)
//	    入队。队列:[e, h]
//	出队 e → 孩子:字符 'r',下面记作 r:
//	    cur=e,cur.fail=root,root 无 'r' → cur=root,cur.fail=nil
//	    → r.fail = root;入队。队列:[h, r]
//	出队 h → 孩子:字符 'e',下面记作 e(这是树里第二个 e,路径 "she"):
//	    cur=h,cur.fail=h(第一层的那个 h),h 有 'e'
//	    → e.fail = e ✓✓(关键一跳,指向第二层的 e,即路径 "he" 的词尾节点)
//	    入队。队列:[r, e]
//	出队 r、出队 e(均无孩子)→ 队列空,结束。
//	结果与文件头总结的 fail 全表完全一致。
func (n *node) build() {
	// 第一层节点的 fail 全部指向根(长度为 1 的串没有真后缀)。
	var nodes []*node
	for _, child := range n.children {
		child.fail = n
		nodes = append(nodes, child)
	}
	for len(nodes) > 0 {
		nd := nodes[0]
		nodes = nodes[1:]
		for key, child := range nd.children {
			nodes = append(nodes, child)
			// 为 child 找 fail:沿着父节点 nd 的 fail 链向上,
			// 找到第一个"拥有 key 子节点"的祖先,child.fail 指向它。
			cur := nd
			for cur != nil {
				if cur.fail == nil {
					// 链到根都没有:fail 指向根。
					child.fail = n
					break
				}
				if fail, ok := cur.fail.children[key]; ok {
					child.fail = fail
					break
				}
				cur = cur.fail
			}
		}
	}
}

// find 扫描文本(chars 为 rune 切片),收集所有关键词命中的区间
// [start, stop)(rune 下标)。一个位置可能命中多个区间,
// fail 链回溯保证全部收集。
//
// 一、每个字符走三步,对应下面三段代码:
//
//	第 1 步 goto:cur 有该字符的孩子 → 直接走过去(顺路);
//	第 2 步 fail:没有 → 沿 fail 链回退(cur = cur.fail),
//	      在哪个节点能"接上"该字符,就从哪继续;
//	      回退到根仍接不上 → child 为 nil,continue 跳过该字符。
//	第 3 步 收取:沿 fail 链向上,每个标了 end 的节点都是一个
//	      以 i 结尾的命中,记区间:start = i+1-child.depth
//	      (depth 即词长),stop = i+1。走到根为止。
//
// 二、两个细节:
//
//  1. 第 2 步循环条件 `cur != n`:根的 fail 是 nil,没法再失配;
//     且循环体是先 cur = cur.fail 再检查,所以根自己的孩子
//     也有机会接上 —— 接上与走到根,都从循环内退出。
//  2. 区间 start 的依据:fail 链上每个节点的 depth 不同
//     (= 后缀长度 = 词长),所以每个 end 节点对应一个
//     不同词长的命中;套公式 start = i+1-depth。
//
// 三、完整扫描示例:文本 "usher"(词典 {"he","her","she"});
// 树里有两个 h、两个 e,按所在层区分:
//
//	i=0 'u': root 无 'u',cur 已是 root → child==nil,continue
//	i=1 's': 第 1 步:root 有 's' → cur=s(第一层)。
//	    第 3 步:child=s,s 非词尾;child=s.fail=root,收取结束。无命中。
//	i=2 'h': 第 1 步:s 有 'h' → cur=h(第二层,路径 "sh")。
//	    第 3 步:child=h,非词尾;child=h.fail=h(第一层的 h),
//	    也非词尾;child=fail=root,收取结束。无命中。
//	i=3 'e': 第 1 步:h 有 'e' → cur=e(第三层,路径 "she"),goto 顺路。
//	    第 3 步:child=e(路径 "she"),e 是词尾
//	    → 记区间 start=3+1-3=1,stop=4,即 "she"[1,4);
//	    child=e.fail=e(第二层,路径 "he"),也是词尾
//	    → 记区间 start=3+1-2=2,stop=4,即 "he"[2,4);
//	    child=e.fail=root,收取结束。一个位置(i=3)揪出两个词。
//	i=4 'r': 第 1 步:e(路径 "she")无 'r' 分支,child=nil。
//	    第 2 步:进 fail 回退循环(条件 cur != root):
//	    cur=cur.fail=e(第二层,路径 "he"),它有 'r' 分支
//	    → cur=r(第三层,路径 "her"),child=r,接上了,跳出循环。
//	    第 3 步:child=r,r 是词尾
//	    → 记区间 start=4+1-3=2,stop=5,即 "her"[2,5);
//	    child=r.fail=root,收取结束。
//	    本步文本指针一动未动,纯靠 fail 链换位,救回 "her"。
//
//	命中结果:[1,4)"she"、[2,4)"he"、[2,5)"her"。
//	复杂度 O(文本长 + 命中数),整个文本只扫一遍。
func (n *node) find(chars []rune) []scope {
	var scopes []scope
	size := len(chars)
	cur := n

	for i := 0; i < size; i++ {
		// 第一步:正常转移(goto)。当前节点有该字符的子节点就走过去。
		child, ok := cur.children[chars[i]]
		if ok {
			cur = child
		} else {
			// 第二步:失配跳转。沿 fail 链回退,直到根;
			// 途中某个 fail 节点若能接上该字符,就从那里继续匹配。
			for cur != n {
				cur = cur.fail
				if child, ok = cur.children[chars[i]]; ok {
					cur = child
					break
				}
			}

			// 回到根也接不上:该字符暂无匹配,跳过。
			if child == nil {
				continue
			}
		}

		// 第三步:收取命中。当前节点沿 fail 链向上,
		// 链上每个 end 节点都对应一个以 i 结尾的关键词命中:
		// 起点 = i+1-depth,终点 = i+1(左闭右开)。
		for child != n {
			if child.end {
				scopes = append(scopes, scope{
					start: i + 1 - child.depth,
					stop:  i + 1,
				})
			}
			child = child.fail
		}
	}

	return scopes
}
