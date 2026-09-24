// ————————————————————————————————————————————————————————————————————————————
// valuer —— 键值读取抽象(配置树的节点) —— 文件总结
//
// Valuer 是"按键取值"的最小接口;valuerWithParent 再加
// Parent() —— 配置树节点知道自己的父节点,支撑 inherit
// (字段向上继承)语义。
//
// 三个实现:
//
//	mapValuer       裸 map 直接查;
//	simpleValuer    只查当前节点;
//	recursiveValuer 查当前节点,没有则沿 Parent() 上溯;
//	                查到 map 时与父节点的同名 map 【合并】
//	                (父的键作底、子的键覆盖)—— 这就是
//	                tag 选项 inherit 的实现机制。
//
// 设计动机:Unmarshaler 的递归填充需要"既能查本层、又能
// 按选项决定是否穿透到上层"的读取器,用接口把这两种
// 策略与遍历逻辑解耦。
// ————————————————————————————————————————————————————————————————————————————
package mapping

type (
	// A Valuer interface defines the way to get values from the underlying object with keys.
	// 键值读取抽象:Unmarshaler 对数据源的唯一依赖。
	Valuer interface {
		// Value gets the value associated with the given key.
		// 按键取值。
		Value(key string) (any, bool)
	}

	// A valuerWithParent defines a node that has a parent node.
	// 带父节点的读取器:支撑向上继承。
	valuerWithParent interface {
		Valuer
		// Parent get the parent valuer for current node.
		// 父节点(根节点返回 nil)。
		Parent() valuerWithParent
	}

	// A node is a map that can use Value method to get values with given keys.
	// 节点:当前层 + 父层。
	node struct {
		// current 当前层读取器。
		current Valuer
		// parent 父层(可空)。
		parent valuerWithParent
	}

	// A valueWithParent is used to wrap the value with its parent.
	// 值与其父层的包装(让子结构递归填充时也能带上文)。
	valueWithParent struct {
		// value 当前值。
		value any
		// parent 值所在节点的父层。
		parent valuerWithParent
	}

	// mapValuer is a type for the map to meet the Valuer interface.
	// 裸 map 适配器。
	mapValuer map[string]any
	// simpleValuer is a type to get value from the current node.
	// 只查当前层的节点。
	simpleValuer node
	// recursiveValuer is a type to get the value recursively from current and parent nodes.
	// 递归上溯的节点(inherit 语义的实现者)。
	recursiveValuer node
)

// Value gets the value associated with the given key from mv.
// map 直接按键取。
func (mv mapValuer) Value(key string) (any, bool) {
	v, ok := mv[key]
	return v, ok
}

// Value gets the value associated with the given key from sv.
// 只查当前层,不穿透。
func (sv simpleValuer) Value(key string) (any, bool) {
	v, ok := sv.current.Value(key)
	return v, ok
}

// Parent get the parent valuer from sv.
// 父节点包装成 recursiveValuer(从父层起就要递归了)。
func (sv simpleValuer) Parent() valuerWithParent {
	if sv.parent == nil {
		return nil
	}

	return recursiveValuer{
		current: sv.parent,
		parent:  sv.parent.Parent(),
	}
}

// Value gets the value associated with the given key from rv,
// and it will inherit the value from parent nodes.
// 递归取值:本层没有 → 沿父链上溯;本层查到 map 时,
// 与父链同名 map 逐层合并(父作底、子覆盖)——
// inherit 选项的落地。
func (rv recursiveValuer) Value(key string) (any, bool) {
	val, ok := rv.current.Value(key)
	if !ok {
		// 本层没有:上溯父链。
		if parent := rv.Parent(); parent != nil {
			return parent.Value(key)
		}

		return nil, false
	}

	// 非 map 的普通值:直接返回,无需合并。
	vm, ok := val.(map[string]any)
	if !ok {
		return val, true
	}

	parent := rv.Parent()
	if parent == nil {
		return val, true
	}

	// 父链上有同名 key 才谈合并。
	pv, ok := parent.Value(key)
	if !ok {
		return val, true
	}

	pm, ok := pv.(map[string]any)
	if !ok {
		return val, true
	}

	// 深度合并:父的键补进子的 map(子的值优先)。
	for k, v := range pm {
		if _, ok := vm[k]; !ok {
			vm[k] = v
		}
	}

	return vm, true
}

// Parent get the parent valuer from rv.
// 父链节点:继续以递归语义包装。
func (rv recursiveValuer) Parent() valuerWithParent {
	if rv.parent == nil {
		return nil
	}

	return recursiveValuer{
		current: rv.parent,
		parent:  rv.parent.Parent(),
	}
}
