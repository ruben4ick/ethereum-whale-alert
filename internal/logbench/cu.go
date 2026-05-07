package logbench

// CUCosts maps JSON-RPC method names to their compute-unit cost on a given provider.
// Defaults below match Alchemy's published pricing table (May 2026 snapshot).
//
// Reference: https://docs.alchemy.com/reference/compute-unit-costs
type CUCosts struct {
	GetLogs               int
	GetBlockReceipts      int
	GetTransactionReceipt int
	GetBlockByNumber      int
}

func DefaultAlchemyCU() CUCosts {
	return CUCosts{
		GetLogs:               75,
		GetBlockReceipts:      500,
		GetTransactionReceipt: 15,
		GetBlockByNumber:      16,
	}
}
