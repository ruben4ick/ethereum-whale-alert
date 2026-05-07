package logbench

// Reference: https://www.alchemy.com/docs/reference/compute-unit-costs
type CUCosts struct {
	GetLogs               int
	GetBlockReceipts      int
	GetTransactionReceipt int
	GetBlockByNumber      int
}

func DefaultAlchemyCU() CUCosts {
	return CUCosts{
		GetLogs:               60,
		GetBlockReceipts:      20,
		GetTransactionReceipt: 20,
		GetBlockByNumber:      20,
	}
}

// DefaultAlchemyThroughputCU returns per-method throughput-CU costs (counts
// against your CU/sec rate limit). Differs from billing CU only for
// eth_getBlockReceipts, which costs 20 to bill but eats 500 throughput CU.
func DefaultAlchemyThroughputCU() CUCosts {
	return CUCosts{
		GetLogs:               60,
		GetBlockReceipts:      500,
		GetTransactionReceipt: 20,
		GetBlockByNumber:      20,
	}
}
