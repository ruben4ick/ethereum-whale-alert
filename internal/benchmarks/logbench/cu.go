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
