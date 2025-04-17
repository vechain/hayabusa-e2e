# drlp

[![Build Status](https://github.com/qianbin/drlp/workflows/Go/badge.svg)](https://github.com/qianbin/drlp/actions)
[![GoDoc](https://godoc.org/github.com/qianbin/drlp?status.svg)](http://godoc.org/github.com/qianbin/drlp)
[![Go Report](https://goreportcard.com/badge/github.com/qianbin/drlp)](https://goreportcard.com/report/github.com/qianbin/drlp)

Short for Direct-RLP: A fast in-place RLP encoder


### Installation

It requires Go 1.19 or newer.

```bash
go get github.com/qianbin/drlp
```

### Usage

Number and string 
```go
var buf []byte
buf = drlp.AppendUint(buf, 10)
buf = drlp.AppendString(buf, []byte("hello drlp"))

fmt.Printf("%x\n", buf)
// Output: 0a8a68656c6c6f2064726c70
```

List
```go
var buf []byte
buf = drlp.AppendString(buf, []byte("followed by a list"))

offset := len(buf)
buf = drlp.AppendString(buf, []byte("list content"))
buf = drlp.EndList(buf, offset)

fmt.Printf("%x\n", buf[offset:])
// Output: cd8c6c69737420636f6e74656e74
```

## License

The MIT License

