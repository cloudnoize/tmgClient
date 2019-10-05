package main

type CyclicBuffer16bit struct {
	buffer     []int16
	readIndex  uint
	writeIndex uint
	size       uint
}

func New16BitCyclicBuffer(size uint) *CyclicBuffer16bit {
	cb := &CyclicBuffer16bit{buffer: make([]int16, size), size: size}
	return cb
}

func (cb *CyclicBuffer16bit) push(s int16) {
	cb.buffer[cb.writeIndex%cb.size] = s
	cb.writeIndex++
}

func (cb *CyclicBuffer16bit) pop() (ret int16) {
	ret = cb.buffer[cb.readIndex%cb.size]
	cb.readIndex++
	return
}
