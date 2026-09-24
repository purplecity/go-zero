// ————————————————————————————————————————————————————————————————————————————
// entropy —— 香农熵(归一化) —— 文件总结
//
// 衡量 map[any]int 计数分布的"混乱程度",返回 [0,1]:
// 1 = 完全随机(各类计数相等),0 = 完全确定(全是一类)。
// go-zero 用在服务发现/负载均衡里评估后端分布是否均衡。
// 两处工程处理:单类/空直接返回 1;概率下限 epsilon 防
// log2(0) = -Inf 污染结果;最后除以 log2(类别数)归一化,
// 让不同类别数的熵可以直接比较。
// ————————————————————————————————————————————————————————————————————————————
package mathx

import "math"

// epsilon 概率下限:防 proba=0 时 log2(0) 发散。
const epsilon = 1e-6

// CalcEntropy calculates the entropy of m.
// 计算归一化香农熵:值 = -Σp·log2(p) / log2(类别数)。
func CalcEntropy(m map[any]int) float64 {
	// 空或只有一类:熵没有意义,按"最随机"处理返回 1。
	if len(m) == 0 || len(m) == 1 {
		return 1
	}

	// 先数总样本量,再算每类的概率 p = count/total。
	var entropy float64
	var total int
	for _, v := range m {
		total += v
	}

	for _, v := range m {
		proba := float64(v) / float64(total)
		if proba < epsilon {
			proba = epsilon
		}
		entropy -= proba * math.Log2(proba)
	}

	// 除以 log2(类别数)归一化到 [0,1],不同类别数可比。
	return entropy / math.Log2(float64(len(m)))
}
