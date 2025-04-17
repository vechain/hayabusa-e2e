package drlp

// AppendUint appends RLP-encoded i to buf and returns the extended buffer.
func AppendUint(buf []byte, i uint64) []byte {
	if i == 0 {
		return append(buf, 0x80)
	} else if i < 128 {
		// fits single byte
		return append(buf, byte(i))
	} else {
		return appendUintWithTag(buf, i, 0x80)
	}
}

// AppendString appends RLP-encoded str to buf and returns the extended buffer.
func AppendString(buf, str []byte) []byte {
	if size := len(str); size == 0 {
		return append(buf, 0x80)
	} else if size == 1 && str[0] < 128 {
		// fits single byte, no string header
		return append(buf, str[0])
	} else if size < 56 {
		buf = append(buf, 0x80+byte(size))
		return append(buf, str...)
	} else {
		buf = appendUintWithTag(buf, uint64(size), 0xB7)
		return append(buf, str...)
	}
}

// EndList ends up list starting from offset and returns the extended buffer.
// Content after offset is treated as list content.
func EndList(buf []byte, offset int) []byte {
	contentSize := len(buf) - offset
	if contentSize == 0 {
		buf = append(buf, 0xC0)
	} else if contentSize < 56 {
		// shift the content for room of list header
		buf = append(buf[:offset+1], buf[offset:]...)
		// write list header
		buf[offset] = 0xC0 + byte(contentSize)
	} else {
		headerSize := uintSize(uint64(contentSize)) + 1
		// shift the content for room of list header
		buf = append(buf[:offset+headerSize], buf[offset:]...)
		// write list header
		appendUintWithTag(buf[:offset], uint64(contentSize), 0xF7)
	}
	return buf
}

// appendUintWithTag appends kind tag and i to b in big endian byte order,
// using the least number of bytes needed to represent i.
func appendUintWithTag(b []byte, i uint64, kindTag byte) []byte {
	switch {
	case i < (1 << 8):
		return append(b, kindTag+1, byte(i))
	case i < (1 << 16):
		return append(b, kindTag+2, byte(i>>8), byte(i))
	case i < (1 << 24):
		return append(b, kindTag+3, byte(i>>16), byte(i>>8), byte(i))
	case i < (1 << 32):
		return append(b, kindTag+4, byte(i>>24), byte(i>>16), byte(i>>8), byte(i))
	case i < (1 << 40):
		return append(b, kindTag+5, byte(i>>32), byte(i>>24), byte(i>>16), byte(i>>8), byte(i))
	case i < (1 << 48):
		return append(b, kindTag+6, byte(i>>40), byte(i>>32), byte(i>>24), byte(i>>16), byte(i>>8), byte(i))
	case i < (1 << 56):
		return append(b, kindTag+7, byte(i>>48), byte(i>>40), byte(i>>32), byte(i>>24), byte(i>>16), byte(i>>8), byte(i))
	default:
		return append(b, kindTag+8, byte(i>>56), byte(i>>48), byte(i>>40), byte(i>>32), byte(i>>24), byte(i>>16), byte(i>>8), byte(i))
	}
}

// uintSize computes the minimum number of bytes required to store i.
func uintSize(i uint64) int {
	switch {
	case i < (1 << 8):
		return 1
	case i < (1 << 16):
		return 2
	case i < (1 << 24):
		return 3
	case i < (1 << 32):
		return 4
	case i < (1 << 40):
		return 5
	case i < (1 << 48):
		return 6
	case i < (1 << 56):
		return 7
	default:
		return 8
	}
}
