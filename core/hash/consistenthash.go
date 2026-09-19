// ————————————————————————————————————————————————————————————————————————————
// 一致性哈希(Consistent Hashing)—— 文件总结
//
// 一、解决什么问题?
//
//	普通哈希取模 hash(key) % N,一旦节点数 N 变化(扩容/缩容),
//	几乎所有 key 的映射结果都会改变,造成缓存大面积失效、数据大规模迁移。
//	一致性哈希把「节点」和「key」都映射到同一个 0 ~ 2^64-1 的哈希环上,
//	key 沿环顺时针遇到的第一个节点就是它的归属节点。这样增删节点时,
//	只有环上相邻一段区间的 key 需要迁移,其余 key 的映射完全不变。
//
// 二、本实现的两个关键设计
//  1. 虚拟节点(virtual replicas):每个真实节点会生成 replicas 个
//     「节点串 + 序号」(如 "node0"、"node1"…)的虚拟节点均匀撒在环上。
//     真实节点数量少时,直接放环上分布会很随机、负载严重不均;
//     虚拟节点越多,各节点占用的环区间越接近,负载越均衡(默认 100 个)。
//     权重功能也是借助虚拟节点实现的:权重 30% 就只放 30% 的虚拟节点。
//  2. 哈希冲突链:多个虚拟节点的哈希值可能相同(虚拟节点成千上万,
//     碰撞并不罕见),所以 ring 是 map[哈希值][]any,同一个环坐标
//     可能挂着多个节点。Get 命中冲突点时,用带盐值的二次哈希做
//     确定性选择,保证同一个 key 永远选到同一个节点。
//
// 三、复杂度
//
//	Get:O(log n) —— keys 有序,二分查找顺时针第一个 >= key 哈希值的点。
//	Add/Remove:O(n log n) —— 增删虚拟节点后要对整个 keys 重新排序。
//
// 四、并发安全
//
//	内部用 sync.RWMutex 保护:Get 加读锁(可并发),Add/Remove 加写锁。
//
// ————————————————————————————————————————————————————————————————————————————
package hash

import (
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/zeromicro/go-zero/core/lang"
)

const (
	// TopWeight 是 AddWithWeight 能设置的最大权重(按百分比理解,100 = 100%)。
	TopWeight = 100

	// minReplicas 是虚拟节点数量的下限。
	// 虚拟节点太少会导致节点在环上分布不均、数据倾斜,
	// 因此即使调用方传入更小的值,也会被强制提升到 100。
	minReplicas = 100

	// prime 是一个质数(FNV-32 哈希使用的质数因子),在这里当作「盐」使用:
	// 当多个节点在环上同一坐标发生冲突时,用 "prime:原始值" 做二次哈希来挑选节点。
	// 加盐是为了让二次哈希结果与第一次哈希解耦,冲突节点间选择更均匀。
	prime = 16777619
)

type (
	// Func 定义了哈希函数的类型,允许调用方自定义哈希算法
	// (默认使用 murmur3,见 hash.go 的 Hash,分布均匀且速度快)。
	Func func(data []byte) uint64

	// A ConsistentHash is a ring hash implementation.
	// ConsistentHash 是一致性哈希环的实现,可并发使用。
	ConsistentHash struct {
		// hashFunc 是计算哈希值所用的函数。
		hashFunc Func
		// replicas 是每个真实节点默认的虚拟节点数量。
		replicas int
		// keys 是哈希环本体:所有虚拟节点的哈希值,升序排列。
		// 有序是为了 Get 时能用二分查找快速定位顺时针方向的下一个节点。
		keys []uint64
		// ring 是「哈希值 -> 节点列表」的映射。
		// 因为不同虚拟节点可能哈希碰撞到同一个值,
		// 所以一个环坐标可能对应多个节点(冲突链)。
		ring map[uint64][]any
		// nodes 是已添加真实节点的字符串表示集合(就是个 set),
		// 用于去重判断和快速判断某节点是否存在于环上。
		nodes map[string]lang.PlaceholderType
		// lock 保护以上所有内部状态;Get 用读锁,Add/Remove 用写锁。
		lock sync.RWMutex
	}
)

// NewConsistentHash returns a ConsistentHash.
// 创建一个默认配置的一致性哈希:每个节点 100 个虚拟节点,使用默认 murmur3 哈希。
func NewConsistentHash() *ConsistentHash {
	return NewCustomConsistentHash(minReplicas, Hash)
}

// NewCustomConsistentHash returns a ConsistentHash with given replicas and hash func.
// 创建自定义配置的一致性哈希。
// replicas:每个节点的虚拟节点数,小于 minReplicas(100)时会被强制提升到 100;
// fn:自定义哈希函数,传 nil 时回退到默认的 murmur3(Hash)。
func NewCustomConsistentHash(replicas int, fn Func) *ConsistentHash {
	// 虚拟节点数量不允许太少,否则哈希环分布不均匀,失去一致性哈希的意义。
	if replicas < minReplicas {
		replicas = minReplicas
	}

	// 哈希函数为空时使用默认实现,保证对象始终可用。
	if fn == nil {
		fn = Hash
	}

	return &ConsistentHash{
		hashFunc: fn,
		replicas: replicas,
		ring:     make(map[uint64][]any),
		nodes:    make(map[string]lang.PlaceholderType),
	}
}

// Add adds the node with the number of h.replicas,
// the later call will overwrite the replicas of the former calls.
// 用默认数量的虚拟节点(h.replicas 个)添加节点。
// 重复添加同一个节点时,后一次调用会覆盖前一次的配置(先删后加)。
func (h *ConsistentHash) Add(node any) {
	h.AddWithReplicas(node, h.replicas)
}

// AddWithReplicas adds the node with the number of replicas,
// replicas will be truncated to h.replicas if it's larger than h.replicas,
// the later call will overwrite the replicas of the former calls.
// 用指定数量的虚拟节点添加节点;replicas 超过 h.replicas 时会被截断。
// 重复添加同一个节点时,后一次调用会覆盖前一次的配置。
func (h *ConsistentHash) AddWithReplicas(node any, replicas int) {
	// 先移除同名节点,实现「重复添加 = 覆盖」的语义:
	// 既能防止旧虚拟节点残留,也避免 keys/ring 中出现重复数据。
	h.Remove(node)

	// 虚拟节点数不能超过上限 h.replicas。
	if replicas > h.replicas {
		replicas = h.replicas
	}

	// 把节点转成字符串表示(支持 Stringer 等),作为哈希的输入。
	nodeRepr := repr(node)
	h.lock.Lock()
	defer h.lock.Unlock()
	h.addNode(nodeRepr)

	// 为该节点生成 replicas 个虚拟节点:
	// 输入是「节点字符串 + 序号」,如 "node-1"+"0"、"node-1"+"1"…,
	// 每个虚拟节点的哈希值就是它在环上的坐标。
	for i := 0; i < replicas; i++ {
		hash := h.hashFunc([]byte(nodeRepr + strconv.Itoa(i)))
		h.keys = append(h.keys, hash)
		h.ring[hash] = append(h.ring[hash], node)
	}

	// 虚拟节点加入后重新排序,维持 keys 升序,
	// 这样 Get 的二分查找(顺时针找下一个节点)才正确。
	sort.Slice(h.keys, func(i, j int) bool {
		return h.keys[i] < h.keys[j]
	})
}

// AddWithWeight adds the node with weight, the weight can be 1 to 100, indicates the percent,
// the later call will overwrite the replicas of the former calls.
// 按权重添加节点,weight 取值 1~100,表示该节点承担的流量百分比。
// 实现方式:权重就是虚拟节点数量的比例(30 权重 = 30% 的虚拟节点)。
// 重复添加同一个节点时,后一次调用会覆盖前一次的配置。
func (h *ConsistentHash) AddWithWeight(node any, weight int) {
	// 不需要检查 weight 是否超过 TopWeight(100):
	// 因为 AddWithReplicas 内部保证 replicas 不会超过 h.replicas,
	// 权重再大也最多等于全量虚拟节点数。
	replicas := h.replicas * weight / TopWeight
	h.AddWithReplicas(node, replicas)
}

// Get returns the corresponding node from h based on the given v.
// 根据给定的 v(一般是 key),从哈希环上找到它归属的节点。
// 规则:沿环顺时针找到第一个哈希值 >= v 的哈希值的虚拟节点。
func (h *ConsistentHash) Get(v any) (any, bool) {
	// 读操作加读锁,多个 Get 可以并发进行,只和写操作互斥。
	h.lock.RLock()
	defer h.lock.RUnlock()

	// 环上没有任何节点,直接返回失败。
	if len(h.ring) == 0 {
		return nil, false
	}

	// 第一步:计算 key 的哈希值,即 key 在环上的坐标。
	hash := h.hashFunc([]byte(repr(v)))

	// 第二步:二分查找 keys 中第一个 >= hash 的位置(顺时针方向)。
	// 若 hash 比环上所有值都大,search 返回 len(keys),
	// 对 len(keys) 取模回到 0,即环首尾相接、绕回最小的哈希值。
	index := sort.Search(len(h.keys), func(i int) bool {
		return h.keys[i] >= hash
	}) % len(h.keys)

	// 第三步:取出该环坐标上的节点列表(哈希冲突时可能多于一个)。
	nodes := h.ring[h.keys[index]]
	switch len(nodes) {
	case 0:
		// 理论上不会发生(有坐标必有节点),防御性返回。
		return nil, false
	case 1:
		// 常见情况:该坐标上只有一个节点,直接返回。
		return nodes[0], true
	default:
		// 哈希冲突:同一坐标上有多个节点,需要做二次选择。
		// 用「prime:原始值」作为输入再哈希一次(与第一次哈希解耦),
		// 对节点数取模选出其中一个。整个过程是确定性的,
		// 保证同一个 key 每次都会选中同一个节点。
		innerIndex := h.hashFunc([]byte(innerRepr(v)))
		pos := int(innerIndex % uint64(len(nodes)))
		return nodes[pos], true
	}
}

// Remove removes the given node from h.
// 从哈希环上移除指定节点及其全部虚拟节点。
func (h *ConsistentHash) Remove(node any) {
	nodeRepr := repr(node)

	h.lock.Lock()
	defer h.lock.Unlock()

	// 节点不存在时直接返回,避免无效操作。
	if !h.containsNode(nodeRepr) {
		return
	}

	// 逐个删除该节点的虚拟节点。
	// 这里循环 h.replicas 次(而不是添加时实际用的次数):
	// 对不存在的虚拟节点,二分查找找不到匹配值会自动跳过,是安全的。
	for i := 0; i < h.replicas; i++ {
		// 按添加时同样的规则算出虚拟节点的哈希值。
		hash := h.hashFunc([]byte(nodeRepr + strconv.Itoa(i)))

		// 二分查找该哈希值在 keys 中的位置,
		// 只有恰好相等时才从环上删除(说明这个虚拟节点确实存在)。
		index := sort.Search(len(h.keys), func(i int) bool {
			return h.keys[i] >= hash
		})
		if index < len(h.keys) && h.keys[index] == hash {
			h.keys = append(h.keys[:index], h.keys[index+1:]...)
		}

		// 再从冲突链(ring)中移除该节点本身。
		h.removeRingNode(hash, nodeRepr)
	}

	// 最后把节点从节点集合中删除。
	h.removeNode(nodeRepr)
}

// removeRingNode 从指定哈希坐标的冲突链中移除 nodeRepr 对应的节点。
// 若移除后链为空,则整个坐标从 ring 中删除,避免 map 无限膨胀。
func (h *ConsistentHash) removeRingNode(hash uint64, nodeRepr string) {
	if nodes, ok := h.ring[hash]; ok {
		// nodes[:0] 复用底层数组,原地过滤,避免额外分配。
		newNodes := nodes[:0]
		for _, x := range nodes {
			// 保留不等于目标节点的元素(通过字符串表示比较)。
			if repr(x) != nodeRepr {
				newNodes = append(newNodes, x)
			}
		}
		if len(newNodes) > 0 {
			h.ring[hash] = newNodes
		} else {
			delete(h.ring, hash)
		}
	}
}

// addNode 把节点的字符串表示加入节点集合(set)。
func (h *ConsistentHash) addNode(nodeRepr string) {
	h.nodes[nodeRepr] = lang.Placeholder
}

// containsNode 判断某个节点(字符串表示)是否已存在于环上。
func (h *ConsistentHash) containsNode(nodeRepr string) bool {
	_, ok := h.nodes[nodeRepr]
	return ok
}

// removeNode 把节点的字符串表示从节点集合中删除。
func (h *ConsistentHash) removeNode(nodeRepr string) {
	delete(h.nodes, nodeRepr)
}

// innerRepr 生成二次哈希的输入:"prime:原始值"。
// 加上质数盐值,使其与第一次哈希的输入不同,
// 冲突场景下的二次选择结果独立且分布均匀。
func innerRepr(node any) string {
	return fmt.Sprintf("%d:%v", prime, node)
}

// repr 返回节点的字符串表示,作为哈希输入。
// 底层是 lang.Repr:优先调用 Stringer.String() 方法,
// 否则按具体类型(指针会自动解引用)转成字符串。
func repr(node any) string {
	return lang.Repr(node)
}
