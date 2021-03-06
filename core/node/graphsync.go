package node

import (
	"github.com/ipfs/go-graphsync"
	gsimpl "github.com/ipfs/go-graphsync/impl"
	"github.com/ipfs/go-graphsync/ipldbridge"
	"github.com/ipfs/go-graphsync/network"
	"github.com/ipfs/go-graphsync/storeutil"
	"github.com/ipfs/go-ipfs-blockstore"
	libp2p "github.com/libp2p/go-libp2p-core"
	"go.uber.org/fx"

	"github.com/TRON-US/go-btfs/core/node/helpers"
)

// Graphsync constructs a graphsync
func Graphsync(lc fx.Lifecycle, mctx helpers.MetricsCtx, host libp2p.Host, bs blockstore.GCBlockstore) graphsync.GraphExchange {
	ctx := helpers.LifecycleCtx(mctx, lc)

	network := network.NewFromLibp2pHost(host)
	ipldBridge := ipldbridge.NewIPLDBridge()
	return gsimpl.New(ctx,
		network, ipldBridge,
		storeutil.LoaderForBlockstore(bs),
		storeutil.StorerForBlockstore(bs),
	)
}
