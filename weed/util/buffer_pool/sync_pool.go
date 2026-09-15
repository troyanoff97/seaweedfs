package buffer_pool

import (
	"bytes"
	"sync"
)

// maxRetainedBufferCap caps the capacity of buffers we hand back to the
// sync.Pool. Oversized buffers are dropped so RSS can recede after upload
// storms (see #6541 / volume ParseUpload pool).
const MaxRetainedBufferCap = 4 * 1024 * 1024

var syncPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

func SyncPoolGetBuffer() *bytes.Buffer {
	return syncPool.Get().(*bytes.Buffer)
}

func SyncPoolPutBuffer(buffer *bytes.Buffer) {
	if buffer == nil {
		return
	}
	if buffer.Cap() > MaxRetainedBufferCap {
		// Drop the buffer; let GC reclaim the oversized backing array.
		return
	}
	buffer.Reset()
	syncPool.Put(buffer)
}
