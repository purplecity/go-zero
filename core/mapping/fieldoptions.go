// ————————————————————————————————————————————————————————————————————————————
// fieldoptions —— 字段 tag 选项的解析结果与求值 —— 文件总结
//
// struct tag 里逗号后的选项(optional/default/range/options/
// env 等)解析成 fieldOptionsWithContext;fieldOptions 额外
// 携带 OptionalDep(optional=xxx 的依赖字段名)。
// 所有取值方法都做 nil 防护(字段没配选项时 opts 为 nil,
// 调用方无需判空)。
//
// toOptionsWithContext 是本文件的核心:求值 optional 的
// 三种形态 ——
//
//	optional            → 恒可选;
//	optional=dep        → dep 与本字段必须同时出现或同时缺席;
//	optional=!dep       → dep 与本字段二选一,必须有值。
//
// 违反约束直接报错;最终产出带上下文(结合当前 Valuer
// 是否有值)的 fieldOptionsWithContext。
// ————————————————————————————————————————————————————————————————————————————
package mapping

import "fmt"

// notSymbol optional 依赖的取反前缀 "!"。
const notSymbol = '!'

type (
	// use context and OptionalDep option to determine the value of Optional
	// nothing to do with context.Context
	// 字段选项(结合上下文求值后):tag 解析结果 + 运行期判定。
	fieldOptionsWithContext struct {
		// Inherit 向上继承父节点的值(见 valuer.go recursiveValuer)。
		Inherit bool
		// FromString 值以字符串形式提供,需按目标类型转换。
		FromString bool
		// Optional 可缺省(不设则缺字段报错)。
		Optional bool
		// Options 枚举白名单(值必须在其中)。
		Options []string
		// Default 缺省时的默认值(字符串形式)。
		Default string
		// EnvVar 缺省时从环境变量取值。
		EnvVar string
		// Range 数值区间限制 [left, right](闭开由标志决定)。
		Range *numberRange
	}

	// fieldOptions tag 直译结果:比 WithContext 版多一个
	// optional 依赖字段名。
	fieldOptions struct {
		fieldOptionsWithContext
		// OptionalDep optional=dep 里的 dep(可带 ! 前缀)。
		OptionalDep string
	}

	// numberRange 数值区间:左右端点 + 是否闭端。
	numberRange struct {
		// left/leftInclude 左端点及是否包含。
		left        float64
		leftInclude bool
		// right/rightInclude 右端点及是否包含。
		right        float64
		rightInclude bool
	}
)

// fromString 是否需要按字符串转换赋值。
func (o *fieldOptionsWithContext) fromString() bool {
	return o != nil && o.FromString
}

// getDefault 取默认值(有则第二个返回值 true)。
func (o *fieldOptionsWithContext) getDefault() (string, bool) {
	if o == nil {
		return "", false
	}

	return o.Default, len(o.Default) > 0
}

// inherit 是否向上继承父节点值。
func (o *fieldOptionsWithContext) inherit() bool {
	return o != nil && o.Inherit
}

// optional 是否可缺省。
func (o *fieldOptionsWithContext) optional() bool {
	return o != nil && o.Optional
}

// options 取枚举白名单。
func (o *fieldOptionsWithContext) options() []string {
	if o == nil {
		return nil
	}

	return o.Options
}

// optionalDep 取 optional 的依赖字段名(可能带 ! 前缀)。
func (o *fieldOptions) optionalDep() string {
	if o == nil {
		return ""
	}

	return o.OptionalDep
}

// toOptionsWithContext 结合当前值上下文求出最终选项:
// 重点是 optional 的三种形态(见文件头);
// dep=!xxx 取反时 baseOn 与 selfOn 必须不同(二选一),
// 正向时必须相同(同进同退)。
func (o *fieldOptions) toOptionsWithContext(key string, m Valuer, fullName string) (
	*fieldOptionsWithContext, error) {
	var optional bool
	if o.optional() {
		dep := o.optionalDep()
		if len(dep) == 0 {
			// 纯 optional:恒可选。
			optional = true
		} else if dep[0] == notSymbol {
			// optional=!dep:二选一,两者不能同有/同无。
			dep = dep[1:]
			if len(dep) == 0 {
				return nil, fmt.Errorf("wrong optional value for %q in %q", key, fullName)
			}

			_, baseOn := m.Value(dep)
			_, selfOn := m.Value(key)
			if baseOn == selfOn {
				return nil, fmt.Errorf("set value for either %q or %q in %q", dep, key, fullName)
			}

			// dep 没值时本字段才可选(必须提供本字段)。
			optional = baseOn
		} else {
			// optional=dep:同进同退。
			_, baseOn := m.Value(dep)
			_, selfOn := m.Value(key)
			if baseOn != selfOn {
				return nil, fmt.Errorf("values for %q and %q should be both provided or both not in %q",
					dep, key, fullName)
			}

			// dep 有值时本字段必须提供(optional=否)。
			optional = !baseOn
		}
	}

	// 求值结果与 tag 直译一致:直接复用原结构。
	if o.fieldOptionsWithContext.Optional == optional {
		return &o.fieldOptionsWithContext, nil
	}

	// 不一致(被依赖关系改写):拷贝一份改 Optional。
	return &fieldOptionsWithContext{
		FromString: o.FromString,
		Optional:   optional,
		Options:    o.Options,
		Default:    o.Default,
		EnvVar:     o.EnvVar,
	}, nil
}
