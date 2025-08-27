module github.com/vechain/hayabusa-e2e

go 1.24.2

replace github.com/syndtr/goleveldb => github.com/vechain/goleveldb v1.0.1-0.20220809091043-51eb019c8655

replace github.com/ethereum/go-ethereum => github.com/vechain/go-ethereum v1.8.15-0.20241126085506-c74017ec91b2

// uncomment to test local versions
//replace github.com/vechain/networkhub => ../networkhub

//replace github.com/vechain/thor/v2 => ../thor

require (
	github.com/cqroot/prompt v0.9.4
	github.com/ethereum/go-ethereum v1.8.14
	github.com/stretchr/testify v1.10.0
	github.com/vechain/thor/v2 v2.3.2-0.20250827150046-83ed5c2af598
)

require (
	github.com/hashicorp/golang-lru v0.0.0-20160813221303-0a025b7e63ad
	github.com/jedib0t/go-pretty/v6 v6.6.7
	github.com/pkg/errors v0.9.1
	github.com/vechain/networkhub v0.0.5-0.20250819120439-71315d6e4883
)

require (
	github.com/aristanetworks/goarista v0.0.0-20250522122834-15d075375ff2 // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/btcsuite/btcd v0.0.0-20171128150713-2e60448ffcc6 // indirect
	github.com/btcsuite/btcd/btcec/v2 v2.3.4 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/bubbles v0.16.1 // indirect
	github.com/charmbracelet/bubbletea v0.24.2 // indirect
	github.com/charmbracelet/lipgloss v0.11.0 // indirect
	github.com/charmbracelet/x/ansi v0.1.1 // indirect
	github.com/containerd/console v1.0.4-0.20230313162750-1ae8d489ac81 // indirect
	github.com/cqroot/multichoose v0.1.1 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1 // indirect
	github.com/go-stack/stack v1.8.1 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/holiman/uint256 v1.3.2 // indirect
	github.com/huin/goupnp v1.3.0 // indirect
	github.com/jackpal/go-nat-pmp v1.0.2 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/mattn/go-sqlite3 v1.14.28 // indirect
	github.com/muesli/ansi v0.0.0-20211018074035-2e021307bc4b // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/reflow v0.3.0 // indirect
	github.com/muesli/termenv v0.15.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_golang v1.22.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.64.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/qianbin/directcache v0.9.7 // indirect
	github.com/qianbin/drlp v0.0.0-20240102101024-e0e02518b5f9 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/rogpeppe/go-internal v1.13.1 // indirect
	github.com/syndtr/goleveldb v1.0.1-0.20220614013038-64ee5596c38a // indirect
	github.com/vechain/go-ecvrf v0.0.0-20220525125849-96fa0442e765 // indirect
	golang.org/x/crypto v0.38.0 // indirect
	golang.org/x/sync v0.14.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/term v0.32.0 // indirect
	golang.org/x/text v0.25.0 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
	gopkg.in/karalabe/cookiejar.v2 v2.0.0-20150724131613-8dcd6a7f4951 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
