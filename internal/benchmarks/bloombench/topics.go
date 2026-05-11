package bloombench

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type Topic struct {
	Name      string
	Hash      common.Hash
	Rarity    string
	Signature string
}

func DefaultTopics() []Topic {
	mk := func(name, sig, rarity string) Topic {
		return Topic{
			Name:      name,
			Hash:      crypto.Keccak256Hash([]byte(sig)),
			Rarity:    rarity,
			Signature: sig,
		}
	}
	return []Topic{
		mk("ERC20_Transfer", "Transfer(address,address,uint256)", "popular"),
		mk("ERC20_Approval", "Approval(address,address,uint256)", "popular"),
		mk("UniV2_Swap", "Swap(address,uint256,uint256,uint256,uint256,address)", "medium"),
		mk("UniV3_Swap", "Swap(address,address,int256,int256,uint160,uint128,int24)", "medium"),
		mk("UniV2_Sync", "Sync(uint112,uint112)", "medium"),
		mk("WETH_Deposit", "Deposit(address,uint256)", "medium"),
		mk("WETH_Withdrawal", "Withdrawal(address,uint256)", "medium"),
		mk("Synthetic_Absent", "BloomBenchSyntheticAbsentEvent_v1(uint256)", "synthetic-absent"),
	}
}
