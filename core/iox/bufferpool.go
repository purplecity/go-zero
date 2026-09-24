// ————————————————————————————————————————————————————————————————————————————
// bufferpool —— bytes.Buffer 对象池 —— 文件总结
//
// sync.Pool 复用 Buffer,减少高频场景的分配/GC 压力。
// 关键在 Put 的容量闸门:Cap() ≥ capability 的"巨型 buffer"
// 不回收 —— 池子若囤住超预期的大块内存,复用反而变成
// 常驻泄漏。Get 时 Reset 清残数据,拿到的都是干净 buffer。
// ————————————————————————————————————————————————————————————————————————————
package iox

import (
	"bytes"
	"sync"
)

// A BufferPool is a pool to buffer bytes.Buffer objects.
// Buffer 对象池:capability 是回收容量上限。
type BufferPool struct {
	// capability 单个 buffer 允许回收的最大容量。
	capability int
	// pool 底层对象池。
	pool *sync.Pool
}

// NewBufferPool returns a BufferPool.
// 创建对象池:超过 capability 容量的 buffer 不予回收。
func NewBufferPool(capability int) *BufferPool {
	return &BufferPool{
		capability: capability,
		pool: &sync.Pool{
			New: func() any {
				return new(bytes.Buffer)
			},
		},
	}
}

// Get returns a bytes.Buffer object from bp.
// 取一个干净 buffer(Reset 清掉上次的残留内容)。
func (bp *BufferPool) Get() *bytes.Buffer {
	buf := bp.pool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// Put returns buf into bp.
// 归还 buffer:容量超过上限的直接丢弃(防大块内存常驻池中),
// nil 静默忽略。
func (bp *BufferPool) Put(buf *bytes.Buffer) {
	if buf == nil {
		return
	}

	if buf.Cap() < bp.capability {
		bp.pool.Put(buf)
	}
}
